package cqappmiddleware

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

// component names this service in logs and spans.
const component = "cqapp-middleware"

// maxRequestBody bounds an inbound body. The published request is three fields.
const maxRequestBody = 64 << 10

// Handler serves the Middleware's quote endpoint.
type Handler struct {
	vendor QuoteSource
	log    *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(vendor QuoteSource, log *slog.Logger) *Handler {
	return &Handler{vendor: vendor, log: log}
}

// Quote validates a request and orchestrates the vendor call.
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		httpx.WriteError(ctx, w, h.log, httpx.Malformed(err))
		return
	}

	req, fieldErrors := Validate(body)
	if len(fieldErrors) > 0 {
		httpx.WriteError(ctx, w, h.log, httpx.Validation(fieldErrors...))
		return
	}

	caller, _ := CallerFrom(ctx)
	h.log.InfoContext(ctx, "quote requested",
		slog.String("staffId", caller.Subject),
		slog.String("jti", caller.TokenID),
		slog.String("loanAmount", req.Amount.String()),
		slog.Int64("loanTermInMonths", req.Months),
		slog.String("riskBand", req.RiskBand),
	)

	quote, err := h.vendor.Quote(ctx, req)
	if err != nil {
		httpx.WriteError(ctx, w, h.log, err)
		return
	}

	h.log.InfoContext(ctx, "quote returned",
		slog.String("staffId", caller.Subject),
		slog.String("quoteId", quote.QuoteID),
		slog.String("commissionRate", quote.CommissionRate.String()),
	)

	// 200, not 201. We create nothing and store nothing, so there is no
	// resource, no Location and nothing to retrieve. See contract.md section 3.
	httpx.WriteJSON(ctx, w, h.log, http.StatusOK, quote)
}

// NewRouter wires the Middleware.
//
// Resilience composes outermost first: breaker, retrier, client. The breaker is
// outside so an open circuit skips the retries entirely, and so it counts one
// outcome per request the user made rather than one per attempt.
func NewRouter(cfg Config, ent Entitlements, log *slog.Logger) http.Handler {
	client := NewVendorClient(cfg.VendorBaseURL, cfg.VendorAPIKey, cfg.VendorTimeout, log)
	resilient := NewBreaker(NewRetrier(client, cfg.Retry, log), cfg.Breaker, log)
	return NewRouterWith(cfg, ent, resilient, log)
}

// NewRouterWith is NewRouter with an injected quote source, so tests can supply
// a fake vendor, or a bare client without resilience, and never touch a network.
func NewRouterWith(cfg Config, ent Entitlements, vendor QuoteSource, log *slog.Logger) http.Handler {
	h := NewHandler(vendor, log)
	verifier := NewVerifier(cfg.SigningKey, cfg.ClockSkew)

	mux := http.NewServeMux()
	// Health is unauthenticated: a check that needs a credential fails for the
	// wrong reasons.
	mux.Handle("GET /healthz", httpx.Health())
	mux.Handle("POST /v1/quotes", httpx.Chain(
		http.HandlerFunc(h.Quote),
		Authenticate(verifier, log),
		Authorise(ent, ScopeQuoteGenerate, log),
	))

	return httpx.Chain(mux,
		telemetryMiddleware(),
		httpx.Correlation(),
		httpx.RequestLogger(log),
		httpx.Recoverer(log),
		httpx.Timeout(cfg.RequestBudget),
	)
}

func telemetryMiddleware() httpx.Middleware {
	return telemetry.Middleware(component)
}
