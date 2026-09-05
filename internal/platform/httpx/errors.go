// Package httpx holds the HTTP conventions shared by every service: the single
// error envelope from contract.md section 5, the middleware chain, and a server
// that shuts down gracefully.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// Error codes. These are the stable identifiers a client may branch on; the
// message is for humans and may be reworded without breaking anyone.
const (
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeUnauthenticated     = "UNAUTHENTICATED"
	CodeForbidden           = "FORBIDDEN"
	CodeUpstreamUnavailable = "UPSTREAM_UNAVAILABLE"
	CodeUpstreamContract    = "UPSTREAM_CONTRACT"
	CodeUpstreamTimeout     = "UPSTREAM_TIMEOUT"
	CodeCircuitOpen         = "UPSTREAM_CIRCUIT_OPEN"
	CodeInternal            = "INTERNAL"
)

// FieldError identifies one failed field. The code is stable so the front end can
// map it to its own wording; see contract.md section 4.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Error is a failure that can be rendered to a caller.
//
// Message states the condition in API terms: what happened, not what a person
// should do about it. These services are internal and have no browser, so a
// message like "sign in again" would be a remedy expressed in terms of a UI that
// may not exist. The BFF owns user facing wording, mapping Code to whatever the
// front end should say; see contract.md section 5.
//
// Message must still be safe to return: no credentials, no hostnames, no
// internal state. Cause carries that detail, is logged, and never leaves the
// process, which is what keeps a vendor credential failure off a staff user's
// screen.
type Error struct {
	Code       string
	Status     int
	Message    string
	Details    []FieldError
	RetryAfter time.Duration

	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.cause)
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.cause }

// Validation reports one or more invalid fields. All failures are reported
// together rather than first failure wins, per contract.md section 4.
func Validation(details ...FieldError) *Error {
	return &Error{
		Code:    CodeValidationFailed,
		Status:  http.StatusBadRequest,
		Message: "request failed validation",
		Details: details,
	}
}

// Malformed reports a body that is not usable JSON.
func Malformed(cause error) *Error {
	return &Error{
		Code:    CodeValidationFailed,
		Status:  http.StatusBadRequest,
		Message: "request body could not be parsed",
		Details: []FieldError{{Field: "body", Code: "malformed_body"}},
		cause:   cause,
	}
}

// Unauthenticated reports a missing or invalid staff session.
func Unauthenticated(cause error) *Error {
	return &Error{
		Code:    CodeUnauthenticated,
		Status:  http.StatusUnauthorized,
		Message: "bearer token missing, invalid or expired",
		cause:   cause,
	}
}

// Forbidden reports a valid caller lacking the required scope.
func Forbidden(cause error) *Error {
	return &Error{
		Code:    CodeForbidden,
		Status:  http.StatusForbidden,
		Message: "caller is not entitled to the required scope",
		cause:   cause,
	}
}

// UpstreamUnavailable reports that the vendor could not produce a quote. This is
// deliberately also the mapping for a rejected vendor api-key: that is our
// operational fault, not the caller's authentication problem, so it must never
// surface as a 401, and the message must not hint that a credential exists.
// See contract.md section 5.
func UpstreamUnavailable(cause error) *Error {
	return &Error{
		Code:    CodeUpstreamUnavailable,
		Status:  http.StatusBadGateway,
		Message: "upstream quote provider unavailable",
		cause:   cause,
	}
}

// UpstreamContract reports a vendor response we could not parse.
func UpstreamContract(cause error) *Error {
	return &Error{
		Code:    CodeUpstreamContract,
		Status:  http.StatusBadGateway,
		Message: "upstream quote provider returned an unexpected response",
		cause:   cause,
	}
}

// UpstreamTimeout reports an exhausted time budget.
func UpstreamTimeout(cause error) *Error {
	return &Error{
		Code:    CodeUpstreamTimeout,
		Status:  http.StatusGatewayTimeout,
		Message: "upstream quote provider timed out",
		cause:   cause,
	}
}

// CircuitOpen reports that calls to the vendor are suspended.
func CircuitOpen(retryAfter time.Duration) *Error {
	return &Error{
		Code:       CodeCircuitOpen,
		Status:     http.StatusServiceUnavailable,
		Message:    "upstream calls suspended by circuit breaker",
		RetryAfter: retryAfter,
	}
}

// Internal reports an unexpected failure. The cause is logged, never rendered.
func Internal(cause error) *Error {
	return &Error{
		Code:    CodeInternal,
		Status:  http.StatusInternalServerError,
		Message: "internal error",
		cause:   cause,
	}
}

type envelope struct {
	Error body `json:"error"`
}

type body struct {
	Code          string       `json:"code"`
	Message       string       `json:"message"`
	Details       []FieldError `json:"details,omitempty"`
	CorrelationID string       `json:"correlationId"`
}

// WriteError renders err as the single error envelope and logs it. Anything that
// is not an *Error is treated as internal, so an unexpected error can never leak
// its text to a caller.
func WriteError(ctx context.Context, w http.ResponseWriter, log *slog.Logger, err error) {
	e, ok := err.(*Error)
	if !ok {
		e = Internal(err)
	}

	if log != nil {
		attrs := []any{slog.String("code", e.Code), slog.Int("status", e.Status)}
		if e.cause != nil {
			attrs = append(attrs, slog.String("cause", e.cause.Error()))
		}
		if e.Status >= http.StatusInternalServerError {
			log.ErrorContext(ctx, "request failed", attrs...)
		} else {
			log.WarnContext(ctx, "request rejected", attrs...)
		}
	}

	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(e.RetryAfter.Seconds())))
	}
	WriteJSON(ctx, w, log, e.Status, envelope{Error: body{
		Code:          e.Code,
		Message:       e.Message,
		Details:       e.Details,
		CorrelationID: logging.CorrelationID(ctx),
	}})
}

// WriteJSON renders v at the given status. A failure to encode is logged; the
// status line has already been sent by then, so there is nothing else to do.
func WriteJSON(ctx context.Context, w http.ResponseWriter, log *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && log != nil {
		log.ErrorContext(ctx, "failed to encode response", slog.String("cause", err.Error()))
	}
}
