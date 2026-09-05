package cqappmiddleware

import (
	"context"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
)

// SetRetrierTiming replaces the Retrier's clock and jitter source. Exported to
// tests only: a resilience test that sleeps for real is slow, and one that
// depends on wall time is flaky.
func SetRetrierTiming(r *Retrier, jitter func(int64) int64, sleep func(context.Context, time.Duration) error, now func() time.Time) {
	r.jitter = jitter
	r.sleep = sleep
	r.now = now
}

// SetBreakerClock replaces the Breaker's clock, so a test can advance time
// rather than wait for it.
func SetBreakerClock(b *Breaker, now func() time.Time) { b.now = now }

// MarkTransient and MarkRequestFault let tests build classified failures without
// standing up a vendor that produces them.
func MarkTransient(e *httpx.Error) error    { return transient(e) }
func MarkRequestFault(e *httpx.Error) error { return requestFault(e) }
