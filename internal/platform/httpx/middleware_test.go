package httpx_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

func TestCorrelationHonoursValidInboundID(t *testing.T) {
	var seen string
	h := httpx.Correlation()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.CorrelationID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(httpx.CorrelationHeader, "upstream-abc123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "upstream-abc123" {
		t.Errorf("context id = %q, want the inbound value", seen)
	}
	if got := rec.Header().Get(httpx.CorrelationHeader); got != "upstream-abc123" {
		t.Errorf("response header = %q, must echo the id", got)
	}
}

func TestCorrelationGeneratesWhenAbsent(t *testing.T) {
	var seen string
	h := httpx.Correlation()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.CorrelationID(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if len(seen) != 32 {
		t.Errorf("expected a generated 32 character id, got %q", seen)
	}
	if rec.Header().Get(httpx.CorrelationHeader) != seen {
		t.Error("generated id must be echoed on the response")
	}
}

// An inbound id is attacker controlled. Anything outside the safe alphabet is
// discarded rather than trimmed, so nothing hostile reaches a log or a header.
func TestCorrelationRejectsUnsafeInboundValues(t *testing.T) {
	tests := map[string]string{
		"header injection": "abc\r\nX-Injected: 1",
		"newline":          "abc\ndef",
		"spaces":           "abc def",
		"quotes":           `abc"def`,
		"too long":         strings.Repeat("a", 65),
		"non ascii":        "abcé",
	}
	for name, hostile := range tests {
		t.Run(name, func(t *testing.T) {
			var seen string
			h := httpx.Correlation()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = logging.CorrelationID(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			// Set directly to bypass the transport's own header validation.
			req.Header[http.CanonicalHeaderKey(httpx.CorrelationHeader)] = []string{hostile}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if seen == hostile {
				t.Errorf("hostile id was accepted: %q", hostile)
			}
			if len(seen) != 32 {
				t.Errorf("expected a generated id to replace it, got %q", seen)
			}
		})
	}
}

func TestCorrelationAcceptsBoundaryLength(t *testing.T) {
	id := strings.Repeat("a", 64)
	var seen string
	h := httpx.Correlation()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.CorrelationID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(httpx.CorrelationHeader, id)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != id {
		t.Errorf("64 characters is within the limit and should be accepted")
	}
}

// A panic must become a safe 500, with the stack in the log and not the body.
func TestRecovererRendersSafeInternalError(t *testing.T) {
	var logs bytes.Buffer
	log := logging.New(logging.Options{Component: "test", Output: &logs})

	h := httpx.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("secret internal state")
		}),
		httpx.Correlation(),
		httpx.Recoverer(log),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret internal state") {
		t.Errorf("panic value leaked to the caller: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "goroutine") {
		t.Error("stack must not appear in the response body")
	}
	if !strings.Contains(logs.String(), "secret internal state") {
		t.Error("panic value must be logged")
	}

	var env struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlationId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("recovered response is not the standard envelope: %v", err)
	}
	if env.Error.Code != httpx.CodeInternal {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.CorrelationID == "" {
		t.Error("a recovered panic must still carry a correlation id for support")
	}
}

func TestChainAppliesOutermostFirst(t *testing.T) {
	var order []string
	mark := func(name string) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mark("first"), mark("second"))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"first", "second", "handler"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestRequestLoggerRecordsStatusAndCorrelation(t *testing.T) {
	var logs bytes.Buffer
	log := logging.New(logging.Options{Component: "test", Output: &logs})

	h := httpx.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}),
		httpx.Correlation(),
		httpx.RequestLogger(log),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quotes", nil)
	req.Header.Set(httpx.CorrelationHeader, "corr-7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	for _, want := range []string{`"status":418`, `"path":"/api/v1/quotes"`, `"httpMethod":"POST"`, `"correlationId":"corr-7"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %s\ngot: %s", want, out)
		}
	}
}

func TestHealthIsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.Health().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}
