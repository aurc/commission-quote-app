# CQ-05 Middleware resilience

## Context

The vendor mock fails 15% of requests and stalls another 10% past the Middleware's budget, because
the challenge requires it to misbehave. Today every one of those reaches the user: running the stack
by hand, roughly one request in six returns `502` or `504`. The brief grades "what happens when the
Commission Quote API times out or throws an error", and the honest answer right now is "the user
sees it".

This task absorbs the failures that are safe to absorb, and only those.

Budgets and the retry rule are already fixed in `design/contract.md` section 6. This implements them.

## The constraint that shapes everything

`assumptions.md` 1.4 assumes quote generation is **not idempotent** at the vendor, so a retry can
create a second quote. A retry is therefore only permitted where the failure proves none was created.

| Retry | Condition |
|---|---|
| yes | Connection refused, DNS failure, reset before response headers |
| yes | Timeout before any response headers arrived |
| yes | `502`, `503`, `504`, the request was rejected at their edge |
| yes | `429`, honouring `Retry-After` when it fits the budget |
| no | `500`, ambiguous: the vendor may have created the quote |
| no | Other `4xx`, deterministic, a retry repeats the result |
| no | `2xx` we could not parse, a quote probably exists |

`500` being non retryable is the whole reason the vendor mock injects `503`.

## Deliverables

```
internal/middleware/
  vendor.go     classify failures as transient or not
  retry.go      bounded backoff with full jitter
  breaker.go    circuit breaker
  handler.go    depend on an interface rather than *VendorClient
```

## Design

### An interface to decorate

`NewRouterWith` currently takes `*VendorClient` concretely, so nothing can wrap it. Introduce:

```go
type QuoteSource interface {
    Quote(ctx context.Context, req QuoteRequest) (Quote, error)
}
```

`VendorClient` satisfies it, and `Retrier` and `Breaker` are decorators over it. Composition order,
outermost first: breaker, retrier, client. The breaker sits outside so an open circuit costs nothing;
inside, it would count each retry as a separate failure and trip on a single bad request.

This is a small refactor of CQ-04 code and the only reason the handler changes.

### Carrying why it failed

`VendorClient.Quote` returns an `*httpx.Error` already translated into our taxonomy, which loses the
reason the retrier needs. Rather than unpick that, the client marks the failures the table above
allows:

```go
var ErrTransient = errors.New("vendor failure produced no quote")
```

wrapped as `fmt.Errorf("%w: %w", ErrTransient, httpErr)`. The retrier asks `errors.Is`, the handler
still recovers the `*httpx.Error` with `errors.As`, and the rule lives in one place next to the
status mapping it belongs to. Anything unmarked is not retried, so the safe default is the default.

### Retrier

Base 150ms, factor 2, full jitter, cap 1s, 3 attempts, all from `contract.md` section 6.

Full jitter, meaning a delay drawn uniformly from `[0, min(cap, base * 2^n))`, rather than fixed
exponential: with a single caller it matters little, but it is the difference between a herd
retrying in lockstep and spreading out, and it costs one line.

The total budget is the context deadline. A sleep is skipped, and the attempt abandoned, when the
remaining budget is shorter than the delay: waiting to make a call we cannot finish wastes the
budget the user is waiting on.

### Breaker

Rolling window 20 requests, minimum 10 samples before it can trip, opens at 50% failures, stays open
10s, then allows 3 half open probes.

What counts as a failure needs deciding, and the table is not obvious:

| Outcome | Counts | Why |
|---|---|---|
| Transport failure, timeout, `5xx` | yes | The vendor is unhealthy |
| `401`, `403` | yes | Our credential is not working; every request will fail, so stop asking |
| `400` | no | Request specific. Tripping on it would block valid requests because of one bad shape |
| Success | no | |

Open returns `503 UPSTREAM_CIRCUIT_OPEN` with `Retry-After`, which already exists in the taxonomy
but has never been reachable.

### Determinism

Both components take an injected clock and jitter source. Tests must not sleep for real, and a
breaker test that depends on wall time is a flaky test. This mirrors `CQAPI_RANDOM_SEED` in the mock.

## Tests

| Area | Cases |
|---|---|
| Retry rule | Each row of the table above, asserting the vendor saw exactly one request or exactly three |
| Non idempotency | A `500` is never retried. This is the test that protects a customer from a duplicate quote |
| Budget | Retries stop when the deadline is near; a slow vendor yields `504` rather than three timeouts |
| Backoff | Delays grow, stay within the cap, and vary with the jitter source |
| Breaker | Opens at the threshold, not before the minimum sample; short circuits while open; recovers through half open; a failing probe reopens it |
| Breaker accounting | `400` does not trip it, `401` does |
| End to end | With the vendor failing 50%, the user sees far fewer failures than the vendor produces |
| Spec | `503` is documented; the conformance test currently forbids it and must be updated with the code |

## Verification

`make check` green. Then by hand, the thing this task exists for: run the stack with
`CQAPI_FAILURE_RATE=0.5` and confirm the user sees a small fraction of that, with the retries visible
in the logs and joined by one `correlationId`.
