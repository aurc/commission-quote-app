package cqapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqapimock"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

const testKey = "test-vendor-key-abcd"

// newServer builds a router with the simulation disabled, so tests exercise the
// contract rather than the dice. Injection has its own tests.
func newServer(t *testing.T, mutate ...func(*cqapi.Config)) http.Handler {
	t.Helper()
	cfg := cqapi.Config{
		APIKey:      testKey,
		FailureRate: 0,
		SlowRate:    0,
		Seed:        1,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	return cqapi.NewRouter(cfg, logging.New(logging.Options{Component: "cqapi-mock", Output: io.Discard}))
}

func post(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(body))
	if key != "" {
		req.Header.Set(cqapi.APIKeyHeader, key)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validBody = `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}`

func TestQuoteHappyPath(t *testing.T) {
	rec := post(t, newServer(t), testKey, validBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var q struct {
		QuoteID         string  `json:"quoteId"`
		CommissionRate  float64 `json:"commissionRate"`
		TotalCommission float64 `json:"totalCommission"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if q.QuoteID == "" {
		t.Error("quoteId must be present")
	}
	if q.CommissionRate != 0.0180 {
		t.Errorf("commissionRate = %v, want 0.0180", q.CommissionRate)
	}
	if q.TotalCommission != 4500.00 {
		t.Errorf("totalCommission = %v, want 4500.00", q.TotalCommission)
	}
}

// The brief requires the key. Missing and wrong must be indistinguishable.
func TestAPIKeyEnforcement(t *testing.T) {
	h := newServer(t)

	missing := post(t, h, "", validBody)
	wrong := post(t, h, "not-the-key-at-all", validBody)

	for name, rec := range map[string]*httptest.ResponseRecorder{"missing": missing, "wrong": wrong} {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s key: status = %d, want 401", name, rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("%s key: body must be empty, got %s", name, rec.Body)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("%s key: must not hint at the scheme, got %q", name, got)
		}
	}
	if missing.Code != wrong.Code || missing.Body.String() != wrong.Body.String() {
		t.Error("a missing key and a wrong key must be indistinguishable")
	}
}

func TestNearMissKeyIsRejected(t *testing.T) {
	h := newServer(t)
	for _, key := range []string{testKey + "x", testKey[:len(testKey)-1], strings.ToUpper(testKey), " " + testKey} {
		if rec := post(t, h, key, validBody); rec.Code != http.StatusUnauthorized {
			t.Errorf("key %q was accepted", key)
		}
	}
}

// Security must not be probabilistic. Even with every request failing, an
// unauthenticated one is still rejected as unauthenticated.
func TestAuthIsEnforcedBeforeFailureInjection(t *testing.T) {
	h := newServer(t, func(c *cqapi.Config) { c.FailureRate = 1.0 })

	for range 20 {
		rec := post(t, h, "", validBody)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 regardless of injected failures", rec.Code)
		}
	}
}

func TestValidationRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"three decimal places", `{"loanAmount":999.999,"loanTermInMonths":12,"riskBand":"A"}`},
		{"exponent notation", `{"loanAmount":1e9,"loanTermInMonths":12,"riskBand":"A"}`},
		{"negative amount", `{"loanAmount":-1000.00,"loanTermInMonths":12,"riskBand":"A"}`},
		{"zero amount", `{"loanAmount":0,"loanTermInMonths":12,"riskBand":"A"}`},
		{"amount as string", `{"loanAmount":"1000","loanTermInMonths":12,"riskBand":"A"}`},
		{"term as string", `{"loanAmount":1000.00,"loanTermInMonths":"12","riskBand":"A"}`},
		{"amount missing", `{"loanTermInMonths":12,"riskBand":"A"}`},
		{"term missing", `{"loanAmount":1000.00,"riskBand":"A"}`},
		{"amount null", `{"loanAmount":null,"loanTermInMonths":12,"riskBand":"A"}`},
		{"fractional term", `{"loanAmount":1000.00,"loanTermInMonths":12.5,"riskBand":"A"}`},
		{"zero term", `{"loanAmount":1000.00,"loanTermInMonths":0,"riskBand":"A"}`},
		{"negative term", `{"loanAmount":1000.00,"loanTermInMonths":-12,"riskBand":"A"}`},
		{"lowercase band", `{"loanAmount":1000.00,"loanTermInMonths":12,"riskBand":"b"}`},
		{"unknown band", `{"loanAmount":1000.00,"loanTermInMonths":12,"riskBand":"D"}`},
		{"empty band", `{"loanAmount":1000.00,"loanTermInMonths":12,"riskBand":""}`},
		{"unknown field", `{"loanAmount":1000.00,"loanTermInMonths":12,"riskBand":"A","extra":1}`},
		{"empty body", ``},
		{"not an object", `[]`},
		{"broken json", `{"loanAmount":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(t, newServer(t), testKey, tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			var e struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
				t.Errorf("expected the vendor error shape, got %s", rec.Body)
			}
		})
	}
}

// The vendor publishes a contract; our business ranges are not part of it. A
// real vendor would not know them, so it must not enforce them.
func TestVendorDoesNotEnforceOurBusinessRanges(t *testing.T) {
	h := newServer(t)

	// Below our minimum and above our maximum: both fine by the vendor.
	for _, body := range []string{
		`{"loanAmount":0.01,"loanTermInMonths":1,"riskBand":"A"}`,
		`{"loanAmount":99000000.00,"loanTermInMonths":600,"riskBand":"C"}`,
	} {
		if rec := post(t, h, testKey, body); rec.Code != http.StatusCreated {
			t.Errorf("vendor should accept %s, got %d: %s", body, rec.Code, rec.Body)
		}
	}
}

// The vendor's error shape is deliberately not our envelope. If this ever starts
// matching, the Middleware has stopped translating.
func TestVendorErrorShapeIsNotOurEnvelope(t *testing.T) {
	rec := post(t, newServer(t), testKey, `{"loanAmount":0,"loanTermInMonths":12,"riskBand":"A"}`)

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err == nil && envelope.Error.Code != "" {
		t.Error("the vendor must not emit our error envelope")
	}
}

func TestHealthNeedsNoCredentialAndIsNeverFailed(t *testing.T) {
	h := newServer(t, func(c *cqapi.Config) { c.FailureRate = 1.0 })

	for range 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("health status = %d, want 200 without a key and without injection", rec.Code)
		}
	}
}

func TestCorrelationIDIsEchoed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/quotes", bytes.NewReader([]byte(validBody)))
	req.Header.Set(cqapi.APIKeyHeader, testKey)
	req.Header.Set("X-Correlation-Id", "corr-vendor-1")
	rec := httptest.NewRecorder()

	newServer(t).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Correlation-Id"); got != "corr-vendor-1" {
		t.Errorf("correlation id = %q, must be propagated through the vendor", got)
	}
}

func TestWrongMethodAndPath(t *testing.T) {
	h := newServer(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/quotes", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/quotes = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", rec.Code)
	}
}
