# CQ-03 Mocked vendor CQAPI

## Context

The vendor Commission Quote API is under construction, so the challenge requires building against a
mock of the agreed contract. This is that mock: a standalone binary that behaves like an external
system we do not control, including refusing us when the key is wrong and failing at random.

It is deliberately built before the Middleware. Having a real, misbehaving vendor to point at means
CQ-04 and CQ-05 are written against observed behaviour rather than a hand written stub.

Contract in `design/contract.md` section 2. This task owns the commission formula; nothing
downstream recomputes it.

It also owns the vendor's OpenAPI file. The brief states the vendor contract is finalized, and
`assumptions.md` says our Middleware spec "will look very similar to the vendor spec" and may diverge
later. That sentence only means something if both specs exist as separate artifacts. Two files make
the divergence point concrete: `api/cqapi.openapi.yaml` is the vendor's, ours is
`api/middleware.openapi.yaml` in CQ-04.

## Deliverables

```
api/cqapi.openapi.yaml       the vendor's published contract
cmd/cqapi-mock/main.go            config load, wiring, serve
internal/cqapimock/
  quote.go                   the formula and money arithmetic
  handler.go                 POST /v1/quotes
  auth.go                    api-key enforcement
  inject.go                  failure and latency simulation
  config.go                  env per contract.md section 9
```

## Design

### Money arithmetic

`totalCommission = round(loanAmount * commissionRate, 2)` in float64 drifts:
`250000.00 * 0.0180` is not exactly `4500.00` in binary floating point.

Compute in integers instead. `loanAmount` becomes cents (`int64`), `commissionRate` becomes
ten thousandths (`int64`). Their product is in units of 1e-6 dollars, divided back to cents with
half up rounding. Exact, and the worst case (5,000,000.00 at the maximum rate) is nowhere near
`int64` overflow.

The request body is decoded with `json.Number`, not `float64`, so `loanAmount` is inspected as text.
That is the only way to tell `999.999` from `1000.00` reliably, and it makes the precision rule in
`contract.md` section 4 checkable rather than approximate.

### Formula

From `contract.md` section 2, owned here:

```
base           = { A: 0.0100, B: 0.0150, C: 0.0225 }[riskBand]
termAdjustment = min(0.0005 * floor(months / 12), 0.0030)
commissionRate = base + termAdjustment
totalCommission = round(loanAmount * commissionRate, 2)
```

The cap binds at 72 months. Anything longer earns the same adjustment, which the tests pin.

### Middleware order

`api-key` is enforced before failure injection. A request with no key must be rejected identically
every time; if injection ran first, an unauthenticated request could sometimes return `503`, which
would make the security behaviour probabilistic and untestable.

Order: correlation, request log, recover, auth, inject, handler.

### api-key enforcement

Missing and wrong both return `401` with an empty body, no `WWW-Authenticate`, nothing that
distinguishes them. Comparison uses `crypto/subtle` so it does not leak length or content by timing.

Note this is the vendor's own envelope, not ours: an empty `401` is what `contract.md` section 2
specifies, and CQ-04 is responsible for translating it into our error taxonomy.

### Failure injection

`503`, not `500`, and deliberately so. Under `contract.md` section 6 a `500` is ambiguous for a non
idempotent operation and therefore not retryable, while `503` honestly signals a request that was
never processed. Injecting `500` would make the mock untestable against our own retry rule.

Seeded `math/rand/v2` behind a mutex, since a `*rand.Rand` is not safe for concurrent use and this
handler is. `CQAPI_RANDOM_SEED` unset means time seeded; set means every run is reproducible, which
is what makes CQ-05's retry tests deterministic.

### quoteId

UUIDv4 from `crypto/rand` with the version and variant bits set, about ten lines. A dependency for
this would not earn its place.

### The vendor spec

`api/cqapi.openapi.yaml` describes `POST /v1/quotes` as the vendor publishes it: the `api-key`
security scheme, the request and response schemas, and `401`, `400` and `503` as the documented
failure responses.

It is the vendor's document, so it describes only what the vendor guarantees. Our business ranges are
absent from it deliberately, for the same reason they are absent from the mock's validation.

Depth, if time holds: a test asserting the handler's responses match this spec, the same guard CQ-04
plans for the Middleware.

### Vendor side validation

The mock validates the shape it publishes: `riskBand` in the enum, `loanAmount` positive with at
most two decimals, `loanTermInMonths` a positive integer. Malformed gets `400`.

It does not enforce our business ranges. Those are ours, they belong in the Middleware, and a real
vendor would not know them. This keeps the authoritative validation boundary honest.

## Tests

| Area | Cases |
|---|---|
| Formula | The worked example, each band, adjustment cap at and beyond 72 months, minimum term |
| Money | Half up rounding, a case where float64 would drift, the maximum amount |
| Auth | Missing and wrong keys are indistinguishable, correct key passes, timing safe compare |
| Ordering | An unauthenticated request is rejected even at a 100% injected failure rate |
| Injection | Rate 1.0 always fails, 0.0 never fails, the same seed reproduces the same sequence |
| Validation | Bad band, negative amount, three decimals, fractional term, empty body |
| Health | Unauthenticated and OK |
| Spec | The committed OpenAPI file parses and covers every response the handler can return |

## Verification

`make check` green. Manually: run with `CQAPI_FAILURE_RATE=1` and confirm `503`; with a wrong key and
confirm an empty `401`; with a fixed seed twice and confirm identical output.
