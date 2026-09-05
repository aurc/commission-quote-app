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
		return httpx.UpstreamTimeout(err)
	}
	if errors.Is(err, context.Canceled) {
		return httpx.UpstreamTimeout(err)
	}
	c.log.ErrorContext(ctx, "vendor call failed", slog.String("cause", err.Error()))
	return httpx.UpstreamUnavailable(err)
}

// translate maps the vendor's response onto ours, per contract.md section 5.
func (c *VendorClient) translate(ctx context.Context, resp *http.Response) (Quote, error) {
	switch resp.StatusCode {
	case http.StatusCreated:
		return c.decode(ctx, resp)

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
		return Quote{}, httpx.UpstreamContract(errors.New("vendor rejected a request we validated"))

	default:
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

// peek returns a bounded snippet of an error body for the log. Bounded because
// an upstream error body is not something we control.
func (c *VendorClient) peek(resp *http.Response) string {
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(b))
}
