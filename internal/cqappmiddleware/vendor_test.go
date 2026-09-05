package cqappmiddleware_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// contract.md section 5, exercised against a fake vendor.
func TestVendorErrorTranslation(t *testing.T) {
	tests := []struct {
		name       string
		vendor     http.HandlerFunc
		wantStatus int
		wantCode   string
	}{
		{
			name: "vendor rejects our api-key",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_UNAVAILABLE",
		},
		{
			name: "vendor forbids us",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_UNAVAILABLE",
		},
		{
			name: "vendor rejects a request we accepted",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"riskBand must be one of A, B, C"}`))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_CONTRACT",
		},
		{
			name: "vendor unavailable",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_UNAVAILABLE",
		},
		{
			name: "vendor server error",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_UNAVAILABLE",
		},
		{
			name: "vendor returns unparseable success",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`not json at all`))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_CONTRACT",
		},
		{
			name: "vendor omits quoteId",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"commissionRate":0.0180,"totalCommission":4500.00}`))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_CONTRACT",
		},
		{
			name: "vendor sends a quoted rate",
			vendor: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"quoteId":"q1","commissionRate":"0.0180","totalCommission":4500.00}`))
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "UPSTREAM_CONTRACT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := quote(t, newMiddleware(t, newFakeVendor(t, tt.vendor)), validRequest)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := errorCode(t, rec); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

// A vendor credential failure must never be presented as the user's session
// problem, and must not hint that a credential exists.
func TestVendorAuthFailureIsNotTheCallersProblem(t *testing.T) {
	var logs testBuffer
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	rec := quote(t, newMiddlewareWithLogs(t, vendor, &logs), validRequest)

	if rec.Code == http.StatusUnauthorized {
		t.Fatal("a vendor api-key failure surfaced as 401 to the caller")
	}
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"key", "credential", "api-key", "unauthor"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response mentions %q: %s", forbidden, rec.Body)
		}
	}
	if !logs.Contains("vendor rejected our credential") {
		t.Error("the real cause must be logged for an operator")
	}
	if logs.Contains(vendorKey) {
		t.Error("the vendor key leaked into the logs in clear")
	}
	if !logs.Contains("****abcd") {
		t.Error("the vendor key should be logged masked so an operator can identify it")
	}
}

// An exhausted budget is a 504, distinct from a vendor that answered badly.
func TestVendorTimeoutIsGatewayTimeout(t *testing.T) {
	vendor := newFakeVendor(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})

	cfg := testConfig()
	cfg.VendorTimeout = 50 * time.Millisecond

	rec := quote(t, newMiddlewareWithConfig(t, vendor, cfg, &testBuffer{}), validRequest)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504: %s", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != "UPSTREAM_TIMEOUT" {
		t.Errorf("code = %q, want UPSTREAM_TIMEOUT", got)
	}
}

func TestVendorUnreachableIsBadGateway(t *testing.T) {
	vendor := okVendor(t)
	cfg := testConfig()
	cfg.VendorBaseURL = "http://127.0.0.1:1" // nothing listens here

	rec := quote(t, newMiddlewareWithConfig(t, vendor, cfg, &testBuffer{}), validRequest)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body)
	}
}

// The credential goes to the vendor and nowhere else.
func TestAPIKeyIsSentToTheVendorAndNeverReturned(t *testing.T) {
	var logs testBuffer
	vendor := okVendor(t)

	rec := quote(t, newMiddlewareWithLogs(t, vendor, &logs), validRequest)

	if got := vendor.lastKey(); got != vendorKey {
		t.Errorf("vendor received api-key %q, want the configured key", got)
	}
	if strings.Contains(rec.Body.String(), vendorKey) {
		t.Error("the vendor key appeared in a response to the caller")
	}
	if logs.Contains(vendorKey) {
		t.Error("the vendor key appeared in the logs in clear")
	}
}

// The vendor owns the formula. A Middleware that recomputed would silently
// disagree with them the day their pricing changes.
func TestCommissionIsPassedThroughNotRecomputed(t *testing.T) {
	// Deliberately inconsistent with the published formula: band B at 240
	// months is 0.0180, and 250000 * 0.0180 is 4500.00, not 1.23 and 99.99.
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"quoteId":"q-surprise","commissionRate":1.2300,"totalCommission":99.99}`))
	})

	rec := quote(t, newMiddleware(t, vendor), validRequest)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["commissionRate"] != 1.23 {
		t.Errorf("commissionRate = %v, want the vendor's 1.23 passed through", got["commissionRate"])
	}
	if got["totalCommission"] != 99.99 {
		t.Errorf("totalCommission = %v, want the vendor's 99.99 passed through", got["totalCommission"])
	}
	if got["quoteId"] != "q-surprise" {
		t.Errorf("quoteId = %v", got["quoteId"])
	}
}

func TestHappyPathReturns200(t *testing.T) {
	rec := quote(t, newMiddleware(t, okVendor(t)), validRequest)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 since we create nothing: %s", rec.Code, rec.Body)
	}
	if rec.Code == http.StatusCreated {
		t.Error("201 claims a resource was created; the app is stateless")
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"quoteId", "commissionRate", "totalCommission"} {
		if _, ok := got[field]; !ok {
			t.Errorf("response is missing %s: %s", field, rec.Body)
		}
	}
}

// The request we send must match the vendor's published contract exactly.
func TestVendorRequestShape(t *testing.T) {
	vendor := okVendor(t)

	quote(t, newMiddleware(t, vendor), `{"loanAmount":1000.5,"loanTermInMonths":12,"riskBand":"A"}`)

	var sent map[string]any
	if err := json.Unmarshal([]byte(vendor.lastRequest()), &sent); err != nil {
		t.Fatalf("vendor request is not JSON: %v\n%s", err, vendor.lastRequest())
	}
	if len(sent) != 3 {
		t.Errorf("vendor request has %d fields, want exactly 3: %s", len(sent), vendor.lastRequest())
	}
	// Normalised to two decimal places on the way out.
	if !strings.Contains(vendor.lastRequest(), "1000.50") {
		t.Errorf("loanAmount should be sent normalised: %s", vendor.lastRequest())
	}
	if sent["riskBand"] != "A" {
		t.Errorf("riskBand = %v", sent["riskBand"])
	}
}

// One request, one trace, one correlation id, all the way to the vendor.
func TestCorrelationIDReachesTheVendor(t *testing.T) {
	var seen string
	vendor := newFakeVendor(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Correlation-Id")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})

	rec := quote(t, newMiddleware(t, vendor), validRequest)

	if seen == "" {
		t.Fatal("no correlation id was propagated to the vendor")
	}
	if got := rec.Header().Get("X-Correlation-Id"); got != seen {
		t.Errorf("response id %q differs from the one sent upstream %q", got, seen)
	}
}
