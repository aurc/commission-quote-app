package cqappmiddleware

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// BreakerConfig configures the circuit breaker, per contract.md section 6.
type BreakerConfig struct {
	// Window is how many recent outcomes are considered.
	Window int
	// MinSamples is how many outcomes are needed before it may trip. Without it,
	// two failures at startup would open the circuit on a 100% failure rate.
	MinSamples int
	// Threshold is the failure fraction that opens it, 0.5 for half.
	Threshold float64
	// OpenFor is how long it stays open before probing.
	OpenFor time.Duration
	// Probes is how many consecutive successes close it again.
	Probes int
}

type breakerState int

const (
	closed breakerState = iota
	open
	halfOpen
)

func (s breakerState) String() string {
	switch s {
	case open:
		return "open"
	case halfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Breaker stops calling a vendor that is failing.
//
// It sits outside the Retrier, not inside. Inside, it would see each retry as a
// separate outcome and could trip on a single unlucky request; outside, it sees
// one outcome per request the user made, and an open circuit costs nothing
// because no retries are attempted at all.
type Breaker struct {
	next QuoteSource
	cfg  BreakerConfig
	log  *slog.Logger
	now  func() time.Time

	mu        sync.Mutex
	state     breakerState
	outcomes  []bool // true is a failure, most recent last
	openedAt  time.Time
	successes int
	inFlight  int
}

// NewBreaker wraps next.
func NewBreaker(next QuoteSource, cfg BreakerConfig, log *slog.Logger) *Breaker {
	return &Breaker{next: next, cfg: cfg, log: log, now: time.Now}
}

// Quote calls the vendor unless the circuit is open.
func (b *Breaker) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	if retryAfter, ok := b.reject(); ok {
		b.log.WarnContext(ctx, "circuit open, not calling the vendor",
			slog.Int64("retryAfterSeconds", int64(retryAfter.Seconds())))
		return Quote{}, circuitOpen(retryAfter)
	}

	quote, err := b.next.Quote(ctx, req)
	b.record(ctx, err)
	return quote, err
}

// reject reports whether the call should be short circuited, and for how long
// the caller should wait.
func (b *Breaker) reject() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == open {
		elapsed := b.now().Sub(b.openedAt)
		if elapsed < b.cfg.OpenFor {
			return b.cfg.OpenFor - elapsed, true
		}
		b.enter(halfOpen)
	}

	if b.state == halfOpen && b.inFlight >= b.cfg.Probes {
		// Probes are a trickle, not a reopening of the floodgates.
		return b.cfg.OpenFor, true
	}

	b.inFlight++
	return 0, false
}

// record folds one outcome into the breaker's view of vendor health.
func (b *Breaker) record(ctx context.Context, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.inFlight--

	// A request the vendor rejected as malformed says nothing about their
	// health. Counting it would let one bad request shape block valid traffic.
	if errors.Is(err, ErrRequestFault) {
		return
	}
	failed := err != nil

	switch b.state {
	case halfOpen:
		if failed {
			b.log.WarnContext(ctx, "probe failed, reopening the circuit")
			b.enter(open)
			return
		}
		b.successes++
		if b.successes >= b.cfg.Probes {
			b.log.InfoContext(ctx, "vendor recovered, closing the circuit")
			b.enter(closed)
		}

	case closed:
		b.outcomes = append(b.outcomes, failed)
		if len(b.outcomes) > b.cfg.Window {
			b.outcomes = b.outcomes[len(b.outcomes)-b.cfg.Window:]
		}
		if b.shouldTrip() {
			b.log.ErrorContext(ctx, "vendor failure rate exceeded the threshold, opening the circuit",
				slog.Int("samples", len(b.outcomes)),
				slog.Float64("threshold", b.cfg.Threshold))
			b.enter(open)
		}

	case open:
		// Raced with another goroutine opening it. Nothing to do.
	}
}

func (b *Breaker) shouldTrip() bool {
	if len(b.outcomes) < b.cfg.MinSamples {
		return false
	}
	failures := 0
	for _, failed := range b.outcomes {
		if failed {
			failures++
		}
	}
	return float64(failures)/float64(len(b.outcomes)) >= b.cfg.Threshold
}

// enter transitions state. The caller holds the lock.
func (b *Breaker) enter(s breakerState) {
	b.state = s
	b.successes = 0
	switch s {
	case open:
		b.openedAt = b.now()
		// Discard the window: it describes a vendor we have stopped calling, and
		// keeping it would let stale failures trip the circuit again immediately
		// after recovery.
		b.outcomes = nil
	case closed:
		b.outcomes = nil
	case halfOpen:
	}
}

// State reports the breaker's state, for tests and diagnostics.
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.String()
}
