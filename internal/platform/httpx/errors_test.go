package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

func write(t *testing.T, err error, ctx context.Context) (*httptest.ResponseRecorder, map[string]any, string) {
	t.Helper()
	var logs bytes.Buffer
	log := logging.New(logging.Options{Component: "test", Output: &logs})
	rec := httptest.NewRecorder()

	httpx.WriteError(ctx, rec, log, err)

	var env struct {
		Error map[string]any `json:"error"`
	}
	if e := json.Unmarshal(rec.Body.Bytes(), &env); e != nil {
		t.Fatalf("body is not JSON: %v\n%s", e, rec.Body.String())
	}
	return rec, env.Error, logs.String()
}

// The status and code for every class in contract.md section 5.
func TestErrorTaxonomyMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        *httpx.Error
		wantStatus int
		wantCode   string
	}{
		{"validation", httpx.Validation(httpx.FieldError{Field: "loanAmount", Code: "amount_out_of_range"}), 400, "VALIDATION_FAILED"},
		{"malformed body", httpx.Malformed(errors.New("bad json")), 400, "VALIDATION_FAILED"},
		{"unauthenticated", httpx.Unauthenticated(nil), 401, "UNAUTHENTICATED"},
		{"forbidden", httpx.Forbidden(nil), 403, "FORBIDDEN"},
		{"vendor unavailable", httpx.UpstreamUnavailable(nil), 502, "UPSTREAM_UNAVAILABLE"},
		{"vendor contract", httpx.UpstreamContract(nil), 502, "UPSTREAM_CONTRACT"},
		{"timeout", httpx.UpstreamTimeout(nil), 504, "UPSTREAM_TIMEOUT"},
		{"circuit open", httpx.CircuitOpen(10 * time.Second), 503, "UPSTREAM_CIRCUIT_OPEN"},
		{"internal", httpx.Internal(nil), 500, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, body, _ := write(t, tt.err, context.Background())

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if body["code"] != tt.wantCode {
				t.Errorf("code = %v, want %s", body["code"], tt.wantCode)
			}
			if msg, _ := body["message"].(string); msg == "" {
				t.Error("every error must carry a message safe to display")
			}
		})
	}
}

// The reason a vendor credential failure is not a 401: it must never be
// presented to a staff user as a session problem.
func TestVendorAuthFailureIsNotReportedAsUnauthenticated(t *testing.T) {
	rec, body, logs := write(t, httpx.UpstreamUnavailable(errors.New("vendor rejected api-key")), context.Background())

	if rec.Code == http.StatusUnauthorized {
		t.Fatal("a vendor api-key failure must not surface as 401")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(strings.ToLower(body["message"].(string)), "key") {
		t.Errorf("user message must not mention the credential: %v", body["message"])
	}
	if !strings.Contains(logs, "vendor rejected api-key") {
		t.Error("the real cause must still be logged")
	}
}

// The cause is for operators, never for callers.
func TestCauseIsLoggedButNeverRendered(t *testing.T) {
	cause := errors.New("dial tcp 10.0.0.5:8083: connection refused")
	rec, _, logs := write(t, httpx.UpstreamUnavailable(cause), context.Background())

	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("internal detail leaked to the caller: %s", rec.Body.String())
	}
	if !strings.Contains(logs, "10.0.0.5") {
		t.Error("the cause must be logged for operators")
	}
}

// A plain error must not have its text rendered to a caller.
func TestUnknownErrorIsTreatedAsInternal(t *testing.T) {
	rec, body, _ := write(t, errors.New("sql: no rows in result set"), context.Background())

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body["code"] != "INTERNAL" {
		t.Errorf("code = %v", body["code"])
	}
	if strings.Contains(rec.Body.String(), "sql:") {
		t.Errorf("unexpected error text leaked: %s", rec.Body.String())
	}
}

func TestValidationDetailsAreRenderedTogether(t *testing.T) {
	err := httpx.Validation(
		httpx.FieldError{Field: "loanAmount", Code: "amount_out_of_range"},
		httpx.FieldError{Field: "riskBand", Code: "risk_band_invalid"},
	)
	_, body, _ := write(t, err, context.Background())

	details, ok := body["details"].([]any)
	if !ok || len(details) != 2 {
		t.Fatalf("expected 2 details reported together, got %v", body["details"])
	}
}

// details is meaningful only for validation, so it must be absent elsewhere.
func TestDetailsOmittedWhenNotValidation(t *testing.T) {
	_, body, _ := write(t, httpx.UpstreamTimeout(nil), context.Background())

	if _, present := body["details"]; present {
		t.Error("details must be omitted for non-validation errors")
	}
}

func TestCorrelationIDIsEchoedInEveryErrorBody(t *testing.T) {
	ctx := logging.WithCorrelationID(context.Background(), "corr-42")
	_, body, _ := write(t, httpx.UpstreamTimeout(nil), ctx)

	if body["correlationId"] != "corr-42" {
		t.Errorf("correlationId = %v, want corr-42", body["correlationId"])
	}
}

func TestCircuitOpenSetsRetryAfter(t *testing.T) {
	rec, _, _ := write(t, httpx.CircuitOpen(10*time.Second), context.Background())

	if got := rec.Header().Get("Retry-After"); got != "10" {
		t.Errorf("Retry-After = %q, want 10", got)
	}
}

func TestServerErrorsLogAtErrorAndClientErrorsDoNot(t *testing.T) {
	var logs bytes.Buffer
	log := logging.New(logging.Options{Component: "test", Output: &logs, Level: slog.LevelDebug})

	httpx.WriteError(context.Background(), httptest.NewRecorder(), log, httpx.Validation())
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Error("a client error must not log at ERROR")
	}

	logs.Reset()
	httpx.WriteError(context.Background(), httptest.NewRecorder(), log, httpx.Internal(errors.New("boom")))
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Error("a server error must log at ERROR")
	}
}

func TestErrorUnwrapsToCause(t *testing.T) {
	cause := errors.New("root")
	if !errors.Is(httpx.UpstreamUnavailable(cause), cause) {
		t.Error("Error must unwrap to its cause")
	}
}
