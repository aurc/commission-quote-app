package cqapi

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
)

// Injector simulates real network conditions, which the challenge requires the
// mock to do.
//
// It runs after api-key enforcement, never before. If it ran first, a request
// with no key would sometimes return 503 instead of 401, which would make the
// security behaviour probabilistic and untestable.
type Injector struct {
	mu  sync.Mutex
	rng *rand.Rand

	failureRate float64
	slowRate    float64
	slowDelay   time.Duration
	latencyMin  time.Duration
	latencyMax  time.Duration
	log         *slog.Logger
}

// NewInjector builds an Injector. A zero seed seeds from the clock; any other
// value makes the whole sequence reproducible, which is what lets CQ-05 test
// retries deterministically.
func NewInjector(cfg Config, log *slog.Logger) *Injector {
	seed := uint64(cfg.Seed)
	if cfg.Seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}
	return &Injector{
		// Not cryptographic, and not meant to be: this is a traffic simulator.
		rng:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), //nolint:gosec // G404: simulation only
		failureRate: cfg.FailureRate,
		slowRate:    cfg.SlowRate,
		slowDelay:   cfg.SlowDelay,
		latencyMin:  cfg.LatencyMin,
		latencyMax:  cfg.LatencyMax,
		log:         log,
	}
}

// draw decides the fate of one request. Both decisions are taken under a single
// lock so the sequence depends only on the seed and the request order, not on
// how the goroutines interleave.
func (i *Injector) draw() (fail, slow bool, latency time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()

	fail = i.rng.Float64() < i.failureRate
	slow = i.rng.Float64() < i.slowRate

	if slow {
		return fail, slow, i.slowDelay
	}
	spread := i.latencyMax - i.latencyMin
	if spread <= 0 {
		return fail, slow, i.latencyMin
	}
	return fail, slow, i.latencyMin + time.Duration(i.rng.Int64N(int64(spread)))
}

// Middleware applies the simulation.
func (i *Injector) Middleware() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fail, slow, latency := i.draw()

			if !sleep(r, latency) {
				// The caller gave up while we were being slow, which is exactly
				// what a timeout looks like from here. Write nothing.
				return
			}

			if fail {
				// 503 rather than 500, deliberately. Under contract.md section 6
				// a 500 is ambiguous for a non idempotent operation and is not
				// retryable, while 503 honestly says the request was never
				// processed. Injecting 500 would make this mock useless for
				// testing our own retry rule.
				i.log.WarnContext(r.Context(), "injected vendor failure",
					slog.Bool("slow", slow),
					slog.Int64("latencyMs", latency.Milliseconds()),
				)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sleep waits for d, or reports false if the request is cancelled first.
func sleep(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.Context().Done():
		return false
	}
}
