package cqappmiddleware

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
)

// RetryConfig bounds retries, per contract.md section 6.
type RetryConfig struct {
	// Attempts includes the first one. 3 means one call and two retries.
	Attempts int
	// Base is the first backoff interval, doubling each attempt.
	Base time.Duration
	// Cap bounds a single backoff interval.
	Cap time.Duration
}

// minAttemptBudget is the time a retry needs to be worth waiting for. Sleeping
// out the remaining budget to make a call that cannot finish spends the time the
// user is waiting on and returns the same failure later.
const minAttemptBudget = 100 * time.Millisecond

// Retrier repeats a call to the vendor, but only for failures that may safely be
// repeated.
//
// Safety, not availability, decides what is repeated. assumptions.md 1.4 assumes
// quote generation is not idempotent at the vendor, so a retry can create a
// second quote. Only failures marked ErrTransient are repeated; everything else
// is returned as it arrived, which makes the safe behaviour the default for any
// failure the classifier has not been taught about.
type Retrier struct {
	next QuoteSource
	cfg  RetryConfig
	log  *slog.Logger

	// jitter and sleep are injected so tests are deterministic and do not wait.
	jitter func(n int64) int64
	sleep  func(ctx context.Context, d time.Duration) error
	now    func() time.Time
}

// NewRetrier wraps next.
func NewRetrier(next QuoteSource, cfg RetryConfig, log *slog.Logger) *Retrier {
	return &Retrier{
		next:   next,
		cfg:    cfg,
		log:    log,
		jitter: func(n int64) int64 { return rand.Int64N(n) }, //nolint:gosec // G404: backoff spread, not a secret
		sleep:  sleepCtx,
		now:    time.Now,
	}
}

// Quote calls the vendor, retrying transient failures within the budget.
func (r *Retrier) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	var lastErr error

	for attempt := 1; attempt <= r.cfg.Attempts; attempt++ {
		quote, err := r.next.Quote(ctx, req)
		if err == nil {
			if attempt > 1 {
				r.log.InfoContext(ctx, "vendor call succeeded after retrying",
					slog.Int("attempt", attempt))
			}
			return quote, nil
		}
		lastErr = err

		if !errors.Is(err, ErrTransient) {
			// Ambiguous or deterministic. Repeating it either risks a duplicate
			// quote or returns the same answer more slowly.
			return Quote{}, err
		}
		if attempt == r.cfg.Attempts {
			break
		}

		delay := r.backoff(attempt, err)
		if !r.worthWaiting(ctx, delay) {
			r.log.WarnContext(ctx, "abandoning retries, budget exhausted",
				slog.Int("attempt", attempt),
				slog.Int64("delayMs", delay.Milliseconds()))
			break
		}

		r.log.WarnContext(ctx, "retrying vendor call",
			slog.Int("attempt", attempt),
			slog.Int64("delayMs", delay.Milliseconds()),
			slog.String("cause", err.Error()))

		if err := r.sleep(ctx, delay); err != nil {
			break
		}
	}

	return Quote{}, lastErr
}

// backoff returns the wait before the next attempt.
//
// Full jitter, a delay drawn uniformly from [0, exponential), rather than the
// exponential itself. With one caller it changes little; with many it is the
// difference between a herd retrying in lockstep and spreading out, and it costs
// one line. A vendor supplied Retry-After wins when it is longer, since they
// know their own recovery better than our formula does.
func (r *Retrier) backoff(attempt int, err error) time.Duration {
	window := r.cfg.Base << (attempt - 1)
	if window > r.cfg.Cap {
		window = r.cfg.Cap
	}

	delay := time.Duration(0)
	if window > 0 {
		delay = time.Duration(r.jitter(int64(window)))
	}

	var httpErr *httpx.Error
	if errors.As(err, &httpErr) && httpErr.RetryAfter > delay {
		return httpErr.RetryAfter
	}
	return delay
}

// worthWaiting reports whether the budget leaves room for the delay and an
// attempt after it.
func (r *Retrier) worthWaiting(ctx context.Context, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return deadline.Sub(r.now()) > delay+minAttemptBudget
}

// sleepCtx waits for d, or returns early if the caller gives up.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
