package cqappmiddleware_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// resilientMiddleware wires the real composition, breaker over retrier over
// client, so these tests exercise what the running service does.
func resilientMiddleware(t *testing.T, vendor *fakeVendor, logs *testBuffer) http.Handler {
	t.Helper()
	cfg := testConfig()
	cfg.VendorBaseURL = vendor.URL
	cfg.Retry = cqappmiddleware.RetryConfig{Attempts: 3, Base: time.Millisecond, Cap: 2 * time.Millisecond}
	cfg.Breaker = cqappmiddleware.BreakerConfig{
		Window: 20, MinSamples: 10, Threshold: 0.5,
		OpenFor: 10 * time.Second, Probes: 3,
	}

	log := logging.New(logging.Options{Component: "middleware", Output: logs})
	client := cqappmiddleware.NewVendorClient(cfg.VendorBaseURL, cfg.VendorAPIKey, cfg.VendorTimeout, log)
	source := cqappmiddleware.NewBreaker(cqappmiddleware.NewRetrier(client, cfg.Retry, log), cfg.Breaker, log)
	return cqappmiddleware.NewRouterWith(cfg, staffFixture(t), source, log)
}

// The point of the whole task: the vendor's failures should mostly not be the
// user's failures.
func TestUsersSeeFarFewerFailuresThanTheVendorProduces(t *testing.T) {
	var n atomic.Int32
	// Every third vendor call fails, so a single attempt would fail about a
	// third of requests.
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1)%3 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})

	h := resilientMiddleware(t, vendor, &testBuffer{})

	const requests = 30
	failures := 0
	for range requests {
		if rec := quote(t, h, validRequest); rec.Code != http.StatusOK {
			failures++
		}
	}

	if failures > 1 {
		t.Errorf("%d of %d requests failed; retries should absorb an intermittent vendor", failures, requests)
	}
}

// An ambiguous failure must reach the user rather than be retried into a
// possible duplicate quote.
func TestAnAmbiguousVendorFailureReachesTheUserUnretried(t *testing.T) {
	var calls atomic.Int32
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	rec := quote(t, resilientMiddleware(t, vendor, &testBuffer{}), validRequest)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("the vendor saw %d requests for one ambiguous failure, want 1", got)
	}
}

// A sustained outage should stop reaching the vendor at all.
func TestSustainedFailureOpensTheCircuitAndStopsCallingTheVendor(t *testing.T) {
	var calls atomic.Int32
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	h := resilientMiddleware(t, vendor, &testBuffer{})

	for range 10 {
		quote(t, h, validRequest)
	}
	callsBefore := calls.Load()

	rec := quote(t, h, validRequest)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 once the circuit is open: %s", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != "UPSTREAM_CIRCUIT_OPEN" {
		t.Errorf("code = %q", got)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("an open circuit must tell the caller when to come back")
	}
	if calls.Load() != callsBefore {
		t.Error("a request reached the vendor while the circuit was open")
	}
}

// Retries must be visible, and joined to the request that caused them.
func TestRetriesAreLoggedAgainstOneCorrelationID(t *testing.T) {
	var n atomic.Int32
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})

	var logs testBuffer
	rec := quote(t, resilientMiddleware(t, vendor, &logs), validRequest)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after a retry: %s", rec.Code, rec.Body)
	}
	if !logs.Contains("retrying vendor call") {
		t.Error("a retry must be visible in the logs")
	}
	if !logs.Contains("vendor call succeeded after retrying") {
		t.Error("the recovery must be visible too")
	}
	if id := rec.Header().Get("X-Correlation-Id"); id == "" || !logs.Contains(id) {
		t.Error("the retry log must carry the request's correlation id")
	}
}

// Validation still runs first: a bad request must not consume retries.
func TestInvalidRequestsAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	})

	quote(t, resilientMiddleware(t, vendor, &testBuffer{}),
		`{"loanAmount":1,"loanTermInMonths":9999,"riskBand":"Z"}`)

	if calls.Load() != 0 {
		t.Errorf("an invalid request reached the vendor %d times", calls.Load())
	}
}
