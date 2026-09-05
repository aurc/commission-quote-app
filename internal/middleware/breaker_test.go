package middleware_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/middleware"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// scriptedSource returns whatever the test tells it to, so breaker behaviour is
// tested without a vendor or a network in the way.
type scriptedSource struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (s *scriptedSource) Quote(context.Context, middleware.QuoteRequest) (middleware.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return middleware.Quote{QuoteID: "q"}, s.err
}

func (s *scriptedSource) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *scriptedSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func breakerOver(t *testing.T, cfg middleware.BreakerConfig) (*scriptedSource, *middleware.Breaker, *fakeClock) {
	t.Helper()
	src := &scriptedSource{}
	log := logging.New(logging.Options{Component: "middleware", Output: io.Discard})
	b := middleware.NewBreaker(src, cfg, log)
	clock := &fakeClock{t: time.Now()}
	middleware.SetBreakerClock(b, clock.now)
	return src, b, clock
}

func testBreakerConfig() middleware.BreakerConfig {
	return middleware.BreakerConfig{
		Window: 20, MinSamples: 10, Threshold: 0.5,
		OpenFor: 10 * time.Second, Probes: 3,
	}
}

func vendorDown() error {
	return middleware.MarkTransient(httpx.UpstreamUnavailable(errors.New("vendor down")))
}

func call(b *middleware.Breaker) error {
	_, err := b.Quote(context.Background(), middleware.QuoteRequest{})
	return err
}

// Two failures at startup are a 100% failure rate. Without a minimum sample
// count the circuit would open on the first blip.
func TestBreakerDoesNotTripBelowTheMinimumSample(t *testing.T) {
	src, b, _ := breakerOver(t, testBreakerConfig())
	src.fail(vendorDown())

	for range 9 {
		_ = call(b)
	}

	if got := b.State(); got != "closed" {
		t.Errorf("state = %s after 9 failures, want closed; the minimum is 10", got)
	}
	if src.count() != 9 {
		t.Errorf("vendor saw %d calls, all should have gone through", src.count())
	}
}

func TestBreakerOpensAtTheThreshold(t *testing.T) {
	src, b, _ := breakerOver(t, testBreakerConfig())
	src.fail(vendorDown())

	for range 10 {
		_ = call(b)
	}

	if got := b.State(); got != "open" {
		t.Fatalf("state = %s after 10 failures, want open", got)
	}

	// While open, nothing reaches the vendor.
	before := src.count()
	err := call(b)
	if src.count() != before {
		t.Error("a call reached the vendor while the circuit was open")
	}

	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != httpx.CodeCircuitOpen {
		t.Fatalf("error = %v, want UPSTREAM_CIRCUIT_OPEN", err)
	}
	if httpErr.RetryAfter <= 0 {
		t.Error("an open circuit must tell the caller how long to wait")
	}
}

func TestBreakerRecoversThroughHalfOpen(t *testing.T) {
	cfg := testBreakerConfig()
	src, b, clock := breakerOver(t, cfg)

	src.fail(vendorDown())
	for range 10 {
		_ = call(b)
	}
	if b.State() != "open" {
		t.Fatal("setup: the circuit should be open")
	}

	clock.advance(cfg.OpenFor)
	src.fail(nil)

	for i := range cfg.Probes {
		if err := call(b); err != nil {
			t.Fatalf("probe %d failed: %v", i+1, err)
		}
	}

	if got := b.State(); got != "closed" {
		t.Errorf("state = %s after %d successful probes, want closed", got, cfg.Probes)
	}
}

func TestAFailingProbeReopensTheCircuit(t *testing.T) {
	cfg := testBreakerConfig()
	src, b, clock := breakerOver(t, cfg)

	src.fail(vendorDown())
	for range 10 {
		_ = call(b)
	}
	clock.advance(cfg.OpenFor)

	// Still broken.
	_ = call(b)

	if got := b.State(); got != "open" {
		t.Errorf("state = %s after a failed probe, want open again", got)
	}
}

// A request the vendor rejected as malformed says nothing about their health.
// Counting it would let one bad request shape block valid traffic.
func TestRequestFaultsDoNotTripTheBreaker(t *testing.T) {
	src, b, _ := breakerOver(t, testBreakerConfig())
	src.fail(middleware.MarkRequestFault(httpx.UpstreamContract(errors.New("vendor rejected it"))))

	for range 30 {
		_ = call(b)
	}

	if got := b.State(); got != "closed" {
		t.Errorf("state = %s, want closed; a malformed request is not a sick vendor", got)
	}
	if src.count() != 30 {
		t.Errorf("vendor saw %d calls, all should have gone through", src.count())
	}
}

// A rejected credential is systemic: every request will fail, so stop asking.
func TestCredentialFailuresDoTripTheBreaker(t *testing.T) {
	src, b, _ := breakerOver(t, testBreakerConfig())
	src.fail(httpx.UpstreamUnavailable(errors.New("vendor rejected api-key")))

	for range 10 {
		_ = call(b)
	}

	if got := b.State(); got != "open" {
		t.Errorf("state = %s, want open; a credential the vendor refuses will refuse every request", got)
	}
}

// Recovery must not be undone by failures recorded before the circuit opened.
func TestStaleFailuresDoNotReopenAfterRecovery(t *testing.T) {
	cfg := testBreakerConfig()
	src, b, clock := breakerOver(t, cfg)

	src.fail(vendorDown())
	for range 10 {
		_ = call(b)
	}
	clock.advance(cfg.OpenFor)

	src.fail(nil)
	for range cfg.Probes {
		_ = call(b)
	}
	if b.State() != "closed" {
		t.Fatal("setup: the circuit should have closed")
	}

	// One failure after recovery must not immediately reopen it.
	src.fail(vendorDown())
	_ = call(b)

	if got := b.State(); got != "closed" {
		t.Errorf("state = %s; one failure after recovery must not reopen a circuit that needs %d samples", got, cfg.MinSamples)
	}
}

func TestBreakerIsSafeUnderConcurrency(t *testing.T) {
	src, b, _ := breakerOver(t, testBreakerConfig())
	src.fail(vendorDown())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = call(b)
		}()
	}
	wg.Wait()

	if got := b.State(); got != "open" {
		t.Errorf("state = %s, want open", got)
	}
}
