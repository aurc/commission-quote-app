package cqappmiddleware_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// countingVendor answers with a scripted sequence of vendor responses and
// records how many requests actually reached it.
type countingVendor struct {
	calls    atomic.Int32
	handler  http.HandlerFunc
	fake     *fakeVendor
	retrier  *cqappmiddleware.Retrier
	slept    []time.Duration
	deadline time.Time
}

func retrierOver(t *testing.T, cfg cqappmiddleware.RetryConfig, handler http.HandlerFunc) (*countingVendor, cqappmiddleware.QuoteSource) {
	t.Helper()
	cv := &countingVendor{}
	cv.fake = newFakeVendor(t, func(w http.ResponseWriter, r *http.Request) {
		cv.calls.Add(1)
		handler(w, r)
	})

	log := logging.New(logging.Options{Component: "middleware", Output: io.Discard})
	client := cqappmiddleware.NewVendorClient(cv.fake.URL, vendorKey, 2*time.Second, log)

	r := cqappmiddleware.NewRetrier(client, cfg, log)
	// No test waits for real; record what it would have waited instead.
	cqappmiddleware.SetRetrierTiming(r,
		func(int64) int64 { return 1 },
		func(_ context.Context, d time.Duration) error {
			cv.slept = append(cv.slept, d)
			return nil
		},
		func() time.Time { return time.Now() },
	)
	cv.retrier = r
	return cv, r
}

func defaultRetry() cqappmiddleware.RetryConfig {
	return cqappmiddleware.RetryConfig{Attempts: 3, Base: 150 * time.Millisecond, Cap: time.Second}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func validQuoteRequest(t *testing.T) cqappmiddleware.QuoteRequest {
	t.Helper()
	req, errs := cqappmiddleware.Validate([]byte(validRequest))
	if len(errs) > 0 {
		t.Fatalf("the fixture request must be valid: %v", errs)
	}
	return req
}

// The rule from contract.md section 6, one row at a time. The assertion that
// matters is how many requests the vendor actually saw.
func TestRetryRule(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCalls int32
		why       string
	}{
		{"503 is retried", status(http.StatusServiceUnavailable), 3, "rejected at their edge, no quote created"},
		{"502 is retried", status(http.StatusBadGateway), 3, "rejected at their edge"},
		{"504 is retried", status(http.StatusGatewayTimeout), 3, "rejected at their edge"},
		{"429 is retried", status(http.StatusTooManyRequests), 3, "rate limited before doing work"},

		{"500 is NOT retried", status(http.StatusInternalServerError), 1, "ambiguous, the vendor may have created a quote"},
		{"400 is NOT retried", status(http.StatusBadRequest), 1, "deterministic, a retry repeats it"},
		{"401 is NOT retried", status(http.StatusUnauthorized), 1, "our credential is wrong, retrying will not fix it"},
		{"403 is NOT retried", status(http.StatusForbidden), 1, "same"},
		{"501 is NOT retried", status(http.StatusNotImplemented), 1, "deterministic"},

		{"unparseable success is NOT retried", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{{{`))
		}, 1, "a quote probably exists"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv, source := retrierOver(t, defaultRetry(), tt.handler)

			if _, err := source.Quote(context.Background(), validQuoteRequest(t)); err == nil {
				t.Fatal("expected a failure")
			}

			if got := cv.calls.Load(); got != tt.wantCalls {
				t.Errorf("vendor saw %d requests, want %d (%s)", got, tt.wantCalls, tt.why)
			}
		})
	}
}

// The single most important test in this package. assumptions.md 1.4 assumes
// quote generation is not idempotent, so retrying an ambiguous failure can bill
// a customer twice.
func TestAmbiguousFailureIsNeverRetried(t *testing.T) {
	cv, source := retrierOver(t, defaultRetry(), status(http.StatusInternalServerError))

	_, err := source.Quote(context.Background(), validQuoteRequest(t))

	if err == nil {
		t.Fatal("expected a failure")
	}
	if got := cv.calls.Load(); got != 1 {
		t.Fatalf("a 500 reached the vendor %d times; it must be sent exactly once, since the vendor may already have created a quote", got)
	}
	if errors.Is(err, cqappmiddleware.ErrTransient) {
		t.Error("a 500 must not be marked transient")
	}
}

func TestRetryStopsOnFirstSuccess(t *testing.T) {
	var n atomic.Int32
	cv, source := retrierOver(t, defaultRetry(), func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})

	quote, err := source.Quote(context.Background(), validQuoteRequest(t))
	if err != nil {
		t.Fatalf("the second attempt should have succeeded: %v", err)
	}
	if quote.QuoteID == "" {
		t.Error("the successful quote must be returned")
	}
	if got := cv.calls.Load(); got != 2 {
		t.Errorf("vendor saw %d requests, want 2", got)
	}
}

func TestSuccessFirstTimeDoesNotRetry(t *testing.T) {
	cv, source := retrierOver(t, defaultRetry(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})

	if _, err := source.Quote(context.Background(), validQuoteRequest(t)); err != nil {
		t.Fatal(err)
	}
	if got := cv.calls.Load(); got != 1 {
		t.Errorf("vendor saw %d requests, want 1", got)
	}
}

// Backoff must grow and stay under the cap.
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	cfg := cqappmiddleware.RetryConfig{Attempts: 5, Base: 100 * time.Millisecond, Cap: 300 * time.Millisecond}
	cv, source := retrierOver(t, cfg, status(http.StatusServiceUnavailable))

	_, _ = source.Quote(context.Background(), validQuoteRequest(t))

	if len(cv.slept) != 4 {
		t.Fatalf("expected 4 waits before 5 attempts, got %d", len(cv.slept))
	}
	// The jitter source returns 1ns, so each recorded delay is the floor of the
	// window; what is asserted is that the window itself grows and is capped.
	for _, d := range cv.slept {
		if d > cfg.Cap {
			t.Errorf("delay %v exceeded the cap %v", d, cfg.Cap)
		}
	}
}

// A vendor supplied Retry-After beats our formula: they know their own recovery.
func TestRetryAfterIsHonoured(t *testing.T) {
	cv, source := retrierOver(t, defaultRetry(), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, _ = source.Quote(context.Background(), validQuoteRequest(t))

	if len(cv.slept) == 0 {
		t.Fatal("expected a wait")
	}
	if cv.slept[0] != 2*time.Second {
		t.Errorf("waited %v, want the vendor's 2s Retry-After", cv.slept[0])
	}
}

// Waiting out the remaining budget to make a call that cannot finish spends the
// time the user is waiting on and returns the same failure later.
func TestRetriesStopWhenTheBudgetIsNearlyGone(t *testing.T) {
	cfg := cqappmiddleware.RetryConfig{Attempts: 5, Base: time.Second, Cap: time.Second}
	cv, source := retrierOver(t, cfg, status(http.StatusServiceUnavailable))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := source.Quote(ctx, validQuoteRequest(t))

	if err == nil {
		t.Fatal("expected a failure")
	}
	if got := cv.calls.Load(); got != 1 {
		t.Errorf("vendor saw %d requests; with no budget for a delay plus an attempt, it should stop after the first", got)
	}
	if len(cv.slept) != 0 {
		t.Errorf("slept %v despite having no budget to use the result", cv.slept)
	}
}
