package cqapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqapimock"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

func statuses(t *testing.T, cfg cqapi.Config, n int) []int {
	t.Helper()
	cfg.APIKey = testKey
	h := cqapi.NewRouter(cfg, logging.New(logging.Options{Component: "cqapi-mock", Output: io.Discard}))

	out := make([]int, 0, n)
	for range n {
		req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(validBody))
		req.Header.Set(cqapi.APIKeyHeader, testKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		out = append(out, rec.Code)
	}
	return out
}

// 503, not 500. Under contract.md section 6 a 500 is ambiguous for a non
// idempotent operation and is not retryable, so injecting one would make this
// mock useless for testing our own retry rule.
func TestInjectedFailuresAre503(t *testing.T) {
	for _, code := range statuses(t, cqapi.Config{FailureRate: 1.0, Seed: 7}, 10) {
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", code)
		}
	}
}

func TestNoFailuresAtZeroRate(t *testing.T) {
	for _, code := range statuses(t, cqapi.Config{FailureRate: 0, Seed: 7}, 20) {
		if code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 with injection disabled", code)
		}
	}
}

// A fixed seed makes the simulation reproducible, which is what lets CQ-05 test
// retries against a vendor that fails predictably.
func TestSameSeedReproducesTheSameSequence(t *testing.T) {
	cfg := cqapi.Config{FailureRate: 0.5, Seed: 42}

	first := statuses(t, cfg, 40)
	second := statuses(t, cfg, 40)

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("request %d differed between runs: %d then %d", i, first[i], second[i])
		}
	}

	// A different seed should not produce the identical sequence, or the seed
	// is not actually being used.
	other := statuses(t, cqapi.Config{FailureRate: 0.5, Seed: 43}, 40)
	same := true
	for i := range first {
		if first[i] != other[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("a different seed produced an identical sequence")
	}
}

func TestFailureRateIsApproximatelyHonoured(t *testing.T) {
	const n = 400
	failures := 0
	for _, code := range statuses(t, cqapi.Config{FailureRate: 0.25, Seed: 11}, n) {
		if code == http.StatusServiceUnavailable {
			failures++
		}
	}
	if rate := float64(failures) / n; rate < 0.15 || rate > 0.35 {
		t.Errorf("observed failure rate %.2f, expected near 0.25", rate)
	}
}

// A slow request must not be answered after the caller has given up. This is the
// behaviour CQ-05's timeout budget is written against.
func TestSlowRequestStopsWhenTheCallerGivesUp(t *testing.T) {
	cfg := cqapi.Config{APIKey: testKey, SlowRate: 1.0, SlowDelay: 2 * time.Second, Seed: 5}
	h := cqapi.NewRouter(cfg, logging.New(logging.Options{Component: "cqapi-mock", Output: io.Discard}))

	req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(validBody))
	req.Header.Set(cqapi.APIKeyHeader, testKey)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler kept working after the caller cancelled")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("nothing should be written to an abandoned request, got %s", rec.Body)
	}
}

func TestLatencyIsBounded(t *testing.T) {
	cfg := cqapi.Config{
		APIKey:     testKey,
		LatencyMin: 10 * time.Millisecond,
		LatencyMax: 30 * time.Millisecond,
		Seed:       3,
	}
	h := cqapi.NewRouter(cfg, logging.New(logging.Options{Component: "cqapi-mock", Output: io.Discard}))

	for range 5 {
		req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(validBody))
		req.Header.Set(cqapi.APIKeyHeader, testKey)

		start := time.Now()
		h.ServeHTTP(httptest.NewRecorder(), req)
		elapsed := time.Since(start)

		if elapsed < cfg.LatencyMin {
			t.Errorf("responded in %v, faster than the configured minimum %v", elapsed, cfg.LatencyMin)
		}
		if elapsed > cfg.LatencyMax+500*time.Millisecond {
			t.Errorf("responded in %v, far beyond the configured maximum %v", elapsed, cfg.LatencyMax)
		}
	}
}
