package bff

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

	"github.com/aurc/commission-quote-app/internal/platform/authtoken"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

// maxBody bounds what we read in either direction. The published request is
// three fields and the response is three more.
const maxBody = 64 << 10

// MiddlewareClient calls the Middleware on a signed in staff member's behalf.
//
// Deliberately not httputil.ReverseProxy. A proxy would forward the Middleware's
// response unchanged, including its message, and the Middleware writes API
// messages rather than user copy. Rewriting the envelope is the reason this
// layer exists rather than a rewrite rule in nginx.
type MiddlewareClient struct {
	baseURL    string
	signingKey secrets.Value
	tokenTTL   time.Duration
	http       *http.Client
	log        *slog.Logger
}

// NewMiddlewareClient builds a client.
func NewMiddlewareClient(baseURL string, signingKey secrets.Value, tokenTTL, timeout time.Duration, log *slog.Logger) *MiddlewareClient {
	return &MiddlewareClient{
		baseURL:    baseURL,
		signingKey: signingKey,
		tokenTTL:   tokenTTL,
		http: &http.Client{
			Transport: telemetry.Transport(nil),
			Timeout:   timeout,
		},
		log: log,
	}
}

// Quote forwards a request body and returns the Middleware's status and a body
// rewritten for the browser.
//
// The token is minted here and nowhere else. It requests quote:generate because
// that is what the caller is asking to do; whether they may is the Middleware's
// decision, taken from its own entitlement source. Checking scopes here would
// rebuild the circularity CQ-04 removed.
func (c *MiddlewareClient) Quote(ctx context.Context, staffID string, body []byte) (int, []byte) {
	token, err := authtoken.Mint(c.signingKey, staffID, []string{authtoken.ScopeQuoteGenerate}, c.tokenTTL)
	if err != nil {
		return c.fail(ctx, httpx.Internal(fmt.Errorf("mint token: %w", err)))
	}

	endpoint, err := url.JoinPath(c.baseURL, "/v1/quotes")
	if err != nil {
		return c.fail(ctx, httpx.Internal(fmt.Errorf("build middleware url: %w", err)))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return c.fail(ctx, httpx.Internal(fmt.Errorf("build middleware request: %w", err)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if id := logging.CorrelationID(ctx); id != "" {
		req.Header.Set(httpx.CorrelationHeader, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.log.ErrorContext(ctx, "middleware call timed out", slog.String("cause", err.Error()))
			return c.fail(ctx, httpx.UpstreamTimeout(err))
		}
		c.log.ErrorContext(ctx, "middleware call failed", slog.String("cause", err.Error()))
		return c.fail(ctx, httpx.UpstreamUnavailable(err))
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return c.fail(ctx, httpx.UpstreamContract(fmt.Errorf("read middleware response: %w", err)))
	}

	if resp.StatusCode == http.StatusOK {
		return http.StatusOK, raw
	}
	return c.rewrite(ctx, resp.StatusCode, raw)
}

// envelope mirrors the shared error shape from contract.md section 5.
type envelope struct {
	Error struct {
		Code          string             `json:"code"`
		Message       string             `json:"message"`
		Details       []httpx.FieldError `json:"details,omitempty"`
		CorrelationID string             `json:"correlationId"`
	} `json:"error"`
}

// rewrite replaces the Middleware's message with user copy, keeping everything
// a client branches on.
func (c *MiddlewareClient) rewrite(ctx context.Context, status int, raw []byte) (int, []byte) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Error.Code == "" {
		c.log.ErrorContext(ctx, "middleware response was not the error envelope",
			slog.Int("middlewareStatus", status))
		return c.fail(ctx, httpx.UpstreamContract(errors.New("middleware returned an unrecognised body")))
	}

	// A 401 from the Middleware means our token was rejected, which is a fault
	// in this service or its configuration, not an expired staff session.
	// Passing it through would send the user to sign in again and change
	// nothing, so it becomes a 502, on the same reasoning that keeps a vendor
	// credential failure off a user's screen.
	if status == http.StatusUnauthorized {
		c.log.ErrorContext(ctx, "the middleware rejected our token",
			slog.String("middlewareCode", env.Error.Code))
		return c.fail(ctx, httpx.UpstreamUnavailable(errors.New("middleware rejected our bearer token")))
	}

	env.Error.Message = userMessage(env.Error.Code)

	out, err := json.Marshal(env)
	if err != nil {
		return c.fail(ctx, httpx.Internal(err))
	}
	return status, out
}

// fail renders an error raised inside the BFF in the same shape.
func (c *MiddlewareClient) fail(ctx context.Context, e *httpx.Error) (int, []byte) {
	var env envelope
	env.Error.Code = e.Code
	env.Error.Message = userMessage(e.Code)
	env.Error.CorrelationID = logging.CorrelationID(ctx)

	out, err := json.Marshal(env)
	if err != nil {
		return http.StatusInternalServerError, []byte(`{"error":{"code":"INTERNAL","message":"Something went wrong. Try again."}}`)
	}
	return e.Status, out
}
