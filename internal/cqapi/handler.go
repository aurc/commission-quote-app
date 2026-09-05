package cqapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/money"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

// quoteRequest is the vendor's request payload.
//
// The numbers are kept as raw JSON, not decoded into float64, so their original
// text survives. contract.md section 4 constrains loanAmount to two decimal
// places, and that is only checkable while the digits are still there: by the
// time 999.999 has become a float64 the extra digit is gone.
//
// Raw also lets us reject a quoted number outright. The decoder is willing to
// read "1000" into a numeric field, and accepting that would mean the schema we
// publish and the schema we enforce quietly differ.
type quoteRequest struct {
	LoanAmount       json.RawMessage `json:"loanAmount"`
	LoanTermInMonths json.RawMessage `json:"loanTermInMonths"`
	RiskBand         string          `json:"riskBand"`
}

// numberText returns the literal text of a JSON number, rejecting a missing
// field and anything that is not an unquoted number.
func numberText(raw json.RawMessage) (string, error) {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return "", errors.New("is required")
	case s[0] == '"':
		return "", errors.New("must be a JSON number, not a string")
	}
	return s, nil
}

// vendorError is the vendor's error shape. It is deliberately not our envelope
// from contract.md section 5. An external system has its own conventions, and
// the Middleware translating them is the whole point of having a Middleware.
type vendorError struct {
	Error string `json:"error"`
}

// Handler serves the vendor's quote endpoint.
type Handler struct {
	log *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(log *slog.Logger) *Handler { return &Handler{log: log} }

// Quote prices a loan.
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req quoteRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.reject(w, r, "request body must be a JSON object matching the quote schema", err)
		return
	}

	amountText, err := numberText(req.LoanAmount)
	if err != nil {
		h.reject(w, r, "loanAmount "+err.Error(), err)
		return
	}
	amount, err := money.ParseAmount(amountText)
	if err != nil {
		h.reject(w, r, "loanAmount must be a decimal amount with at most 2 decimal places", err)
		return
	}
	// The vendor publishes a two decimal place amount, so it enforces that much.
	// It does not enforce our business ranges: see below.
	if amount.DecimalPlaces() > 2 {
		h.reject(w, r, "loanAmount must be a decimal amount with at most 2 decimal places",
			errors.New("too many decimal places"))
		return
	}
	if amount.Sign() <= 0 {
		h.reject(w, r, "loanAmount must be greater than zero", errors.New("non positive amount"))
		return
	}

	termText, err := numberText(req.LoanTermInMonths)
	if err != nil {
		h.reject(w, r, "loanTermInMonths "+err.Error(), err)
		return
	}
	months, err := json.Number(termText).Int64()
	if err != nil {
		h.reject(w, r, "loanTermInMonths must be a whole number of months", err)
		return
	}
	if months <= 0 {
		h.reject(w, r, "loanTermInMonths must be greater than zero", errors.New("non positive term"))
		return
	}

	// The vendor validates the shape it publishes and nothing more. Our business
	// ranges are ours; a real vendor would not know them, and enforcing them here
	// would quietly move the authoritative validation boundary.
	band := RiskBand(req.RiskBand)
	if _, ok := baseRates[band]; !ok {
		h.reject(w, r, fmt.Sprintf("riskBand must be one of A, B, C, got %q", req.RiskBand),
			errors.New("unknown risk band"))
		return
	}

	quote, err := Generate(amount, months, band)
	if err != nil {
		h.log.ErrorContext(ctx, "failed to generate quote", slog.String("cause", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	h.log.InfoContext(ctx, "quote generated",
		slog.String("quoteId", quote.QuoteID),
		slog.String("loanAmount", amount.String()),
		slog.Int64("loanTermInMonths", months),
		slog.String("riskBand", string(band)),
		slog.Float64("commissionRate", quote.CommissionRate.Float()),
	)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(quote); err != nil {
		h.log.ErrorContext(ctx, "failed to encode quote", slog.String("cause", err.Error()))
	}
}

// reject renders the vendor's 400.
func (h *Handler) reject(w http.ResponseWriter, r *http.Request, message string, cause error) {
	h.log.WarnContext(r.Context(), "rejected malformed vendor request",
		slog.String("reason", message),
		slog.String("cause", cause.Error()),
	)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(vendorError{Error: message})
}

// NewRouter wires the vendor mock.
//
// api-key enforcement and simulation are applied per route, not globally, so
// /healthz stays reachable without a credential and is never randomly failed. A
// health check that needs a secret, or that fails 15% of the time, tells an
// orchestrator nothing useful.
func NewRouter(cfg Config, log *slog.Logger) http.Handler {
	h := NewHandler(log)
	injector := NewInjector(cfg, log)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpx.Health())
	mux.Handle("POST /v1/quotes", httpx.Chain(
		http.HandlerFunc(h.Quote),
		RequireAPIKey(cfg.APIKey, log),
		injector.Middleware(),
	))

	// Telemetry first so a span exists before the correlation id is chosen; the
	// recoverer sits inside the request logger so a panic is still logged with
	// its status.
	return httpx.Chain(mux,
		telemetry.Middleware("cqapi"),
		httpx.Correlation(),
		httpx.RequestLogger(log),
		httpx.Recoverer(log),
	)
}
