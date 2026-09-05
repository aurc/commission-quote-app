package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/money"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

// apiKeyHeader is the credential the vendor requires. It is attached here and
// nowhere else: this is the only component that holds it.
const apiKeyHeader = "api-key"

// maxVendorBody bounds what we will read from the vendor. An upstream that
// streams without end should not be able to exhaust our memory.
const maxVendorBody = 1 << 20

// QuoteSource asks the vendor for a quote. The handler depends on this rather
// than on the client, so resilience can be layered over it: the running service
// composes breaker(retrier(client)).
type QuoteSource interface {
	Quote(ctx context.Context, req QuoteRequest) (Quote, error)
}

// Classification markers. They travel alongside the translated *httpx.Error, so
// the response mapping stays in one place and the policy questions are answered
// where the vendor's behaviour is already being interpreted.
var (
	// ErrTransient marks a failure that provably produced no quote, or one the
	// design has accepted regenerating. Only these may be retried, and anything
	// unmarked is not, so the safe default is the default. See contract.md
	// section 6.
	ErrTransient = errors.New("vendor failure produced no quote")

	// ErrRequestFault marks a failure caused by the request we sent rather than
	// by the vendor's health. The circuit breaker ignores these: one badly
	// shaped request must not stop valid traffic.
	ErrRequestFault = errors.New("vendor rejected this request")
)

func transient(e *httpx.Error) error {
	return fmt.Errorf("%w: %w", ErrTransient, e)
}

// circuitOpen is the response when we decline to call the vendor at all.
func circuitOpen(retryAfter time.Duration) error {
	return httpx.CircuitOpen(retryAfter)
}

func requestFault(e *httpx.Error) error {
	return fmt.Errorf("%w: %w", ErrRequestFault, e)
}

// Quote is what we return to our caller.
type Quote struct {
	QuoteID         string       `json:"quoteId"`
	CommissionRate  money.Rate   `json:"commissionRate"`
	TotalCommission money.Amount `json:"totalCommission"`
}

// vendorQuote mirrors the vendor's response. The numbers are raw so they can be
// parsed exactly, the same way request amounts are.
type vendorQuote struct {
	QuoteID         string          `json:"quoteId"`
	CommissionRate  json.RawMessage `json:"commissionRate"`
	TotalCommission json.RawMessage `json:"totalCommission"`
}

// VendorClient calls the vendor Commission Quote API.
//
// One attempt, one timeout. Retries, backoff and the circuit breaker are CQ-05
// and wrap this; nothing here anticipates them beyond keeping the call in one
// place.
type VendorClient struct {
	baseURL string
	apiKey  secrets.Value
	http    *http.Client
	log     *slog.Logger
}

// NewVendorClient builds a client. The transport is instrumented so the vendor
// call is a span on the same trace as the inbound request.
func NewVendorClient(baseURL string, apiKey secrets.Value, timeout time.Duration, log *slog.Logger) *VendorClient {
	return &VendorClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Transport: telemetry.Transport(nil),
			Timeout:   timeout,
		},
		log: log,
	}
}

// Quote asks the vendor to price a loan.
//
// Every failure is returned as an *httpx.Error already translated into our
// taxonomy, so the handler never has to reason about the vendor's conventions.
func (c *VendorClient) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	body, err := json.Marshal(map[string]any{
		"loanAmount":       json.RawMessage(req.Amount.String()),
		"loanTermInMonths": req.Months,
		"riskBand":         req.RiskBand,
	})
	if err != nil {
		return Quote{}, httpx.Internal(fmt.Errorf("encode vendor request: %w", err))
	}

	endpoint, err := url.JoinPath(c.baseURL, "/v1/quotes")
	if err != nil {
		return Quote{}, httpx.Internal(fmt.Errorf("build vendor url: %w", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Quote{}, httpx.Internal(fmt.Errorf("build vendor request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(apiKeyHeader, c.apiKey.Reveal())
	if id := logging.CorrelationID(ctx); id != "" {
		httpReq.Header.Set(httpx.CorrelationHeader, id)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Quote{}, c.transportError(ctx, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxVendorBody))
		_ = resp.Body.Close()
	}()

	return c.translate(ctx, resp)
}

// transportError classifies a failure that happened before a response arrived.
func (c *VendorClient) transportError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		c.log.ErrorContext(ctx, "vendor call timed out", slog.String("cause", err.Error()))
		// Strictly a timeout is ambiguous: the vendor may have created a quote
		// we never saw. It is retried anyway because assumptions.md 1.5 says
		// exactly that case is regenerated, quotes being advisory. See the
		// reasoning in contract.md section 6.
		return transient(httpx.UpstreamTimeout(err))
	}
	if errors.Is(err, context.Canceled) {
		// The caller gave up. Retrying would spend a budget nobody is waiting on.
		return httpx.UpstreamTimeout(err)
	}
	// Connection refused, DNS failure, reset before headers: the request never
	// reached their handler, so no quote exists.
	c.log.ErrorContext(ctx, "vendor call failed", slog.String("cause", err.Error()))
	return transient(httpx.UpstreamUnavailable(err))
}

// translate maps the vendor's response onto ours, per contract.md section 5.
func (c *VendorClient) translate(ctx context.Context, resp *http.Response) (Quote, error) {
	switch resp.StatusCode {
	case http.StatusCreated:
		return c.decode(ctx, resp)

	case http.StatusTooManyRequests:
		// Rate limited before doing any work. Their Retry-After wins over our
		// backoff when they bothered to send one.
		e := httpx.UpstreamUnavailable(errors.New("vendor rate limited us"))
		e.RetryAfter = retryAfter(resp)
		c.log.WarnContext(ctx, "vendor rate limited us",
			slog.Int64("retryAfterSeconds", int64(e.RetryAfter.Seconds())))
		return Quote{}, transient(e)

	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// Rejected at their edge, so their handler never ran. The safest class
		// to retry, and the reason the mock injects 503 rather than 500.
		c.log.WarnContext(ctx, "vendor unavailable", slog.Int("vendorStatus", resp.StatusCode))
		return Quote{}, transient(httpx.UpstreamUnavailable(fmt.Errorf("vendor returned %d", resp.StatusCode)))

	case http.StatusUnauthorized, http.StatusForbidden:
		// Our credential is wrong, not the caller's. Reporting 401 here would
		// tell a staff member their session expired, which is false, and would
		// leak that a credential exists at all. Logged at error because it needs
		// an operator, not a user.
		c.log.ErrorContext(ctx, "vendor rejected our credential",
			slog.Int("vendorStatus", resp.StatusCode),
			slog.String("apiKey", c.apiKey.String()),
		)
		return Quote{}, httpx.UpstreamUnavailable(fmt.Errorf("vendor rejected api-key with %d", resp.StatusCode))

	case http.StatusBadRequest:
		// We validated and accepted; they rejected. Our validation and theirs
		// have drifted apart, which is our bug and not the caller's mistake, so
		// it must not come back as a 400.
		c.log.ErrorContext(ctx, "vendor rejected a request we accepted, validation has drifted",
			slog.String("vendorBody", c.peek(resp)),
		)
		// Not transient, and not the vendor's health either: retrying repeats the
		// same rejection, and tripping the breaker would block valid requests
		// because of one bad shape.
		return Quote{}, requestFault(httpx.UpstreamContract(errors.New("vendor rejected a request we validated")))

	default:
		// Everything else, 500 included. A 500 is deliberately not retried: it
		// is ambiguous, since the vendor may have created a quote before
		// failing, and a retry is as likely to hit the same fault. See
		// contract.md section 6.
		c.log.ErrorContext(ctx, "vendor returned an unexpected status",
			slog.Int("vendorStatus", resp.StatusCode),
		)
		return Quote{}, httpx.UpstreamUnavailable(fmt.Errorf("vendor returned %d", resp.StatusCode))
	}
}

// decode reads a successful vendor response.
//
// The commission is never recomputed. The vendor owns the formula, and a
// Middleware that recalculated would silently disagree with them the day their
// pricing changes. We check the shape, not the arithmetic.
func (c *VendorClient) decode(ctx context.Context, resp *http.Response) (Quote, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVendorBody))
	if err != nil {
		return Quote{}, httpx.UpstreamContract(fmt.Errorf("read vendor response: %w", err))
	}

	var vq vendorQuote
	if err := json.Unmarshal(body, &vq); err != nil {
		c.log.ErrorContext(ctx, "vendor response is not valid JSON", slog.String("cause", err.Error()))
		return Quote{}, httpx.UpstreamContract(err)
	}
	if vq.QuoteID == "" {
		return Quote{}, httpx.UpstreamContract(errors.New("vendor response has no quoteId"))
	}

	rateText, ok := numberText(vq.CommissionRate)
	if !ok {
		return Quote{}, httpx.UpstreamContract(errors.New("vendor commissionRate is not a number"))
	}
	rate, err := money.ParseRate(rateText)
	if err != nil {
		return Quote{}, httpx.UpstreamContract(fmt.Errorf("vendor commissionRate: %w", err))
	}

	totalText, ok := numberText(vq.TotalCommission)
	if !ok {
		return Quote{}, httpx.UpstreamContract(errors.New("vendor totalCommission is not a number"))
	}
	total, err := money.ParseAmount(totalText)
	if err != nil {
		return Quote{}, httpx.UpstreamContract(fmt.Errorf("vendor totalCommission: %w", err))
	}

	return Quote{QuoteID: vq.QuoteID, CommissionRate: rate, TotalCommission: total}, nil
}

// retryAfter reads the vendor's Retry-After header, in seconds. Zero when
// absent or unusable, in which case our own backoff applies.
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// peek returns a bounded snippet of an error body for the log. Bounded because
// an upstream error body is not something we control.
func (c *VendorClient) peek(resp *http.Response) string {
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(b))
}
