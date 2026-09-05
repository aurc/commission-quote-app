# Commission Quote App: Implementation Contract

`assumptions.md` states the *why*: posture, trade-offs, scope. This document states the *what*:
the values and rules code is written and tested against. Where the two disagree, this document wins
for behaviour and `assumptions.md` wins for intent.

Every value here is an MVP decision, not a vendor mandate. The challenge fixed only the field names
and the `api-key` requirement.

## 1. Domain

### riskBand

Enum, uppercase, single letter. `A` | `B` | `C`.

| Band | Meaning        |
|------|----------------|
| A    | Low risk       |
| B    | Standard risk  |
| C    | Elevated risk  |

Assumption: the challenge does not define the domain. Three bands keep the FE control and the test
matrix small. Adding bands is a data change, not a code change.

### Money

AUD only. Amounts are JSON numbers with at most 2 decimal places. Rounding is half up.
Rates are JSON numbers with 4 decimal places (`0.0125` is 1.25%).

## 2. Vendor contract (CQAPI)

`POST /v1/quotes`

Headers: `api-key` (required), `Content-Type: application/json`, `X-Correlation-Id`, `traceparent`.

Request:
```json
{ "loanAmount": 250000.00, "loanTermInMonths": 240, "riskBand": "B" }
```

Response `201`:
```json
{ "quoteId": "b3f1c9e2-...", "commissionRate": 0.0165, "totalCommission": 4125.00 }
```

`quoteId` is a UUIDv4 generated per request. Quotes are not stored.

### Commission formula

Owned by the vendor. The Middleware never recomputes or validates the arithmetic; it passes the
result through. This is the boundary that matters when the vendor is replaced.

```
base = { A: 0.0100, B: 0.0150, C: 0.0225 }[riskBand]
termAdjustment = min(0.0005 * floor(loanTermInMonths / 12), 0.0030)
commissionRate = round(base + termAdjustment, 4)
totalCommission = round(loanAmount * commissionRate, 2)
```

Worked example: 250000.00, 240 months, band B gives base 0.0150, adjustment min(0.0005*20, 0.0030)
= 0.0030, rate 0.0180, total 4500.00.

### Failure simulation

The challenge requires occasional random errors. Injection is applied before the handler and is
seedable so tests are deterministic.

| Behaviour        | Env var                | Default | Effect                                     |
|------------------|------------------------|---------|--------------------------------------------|
| Error rate       | `CQAPI_FAILURE_RATE`   | `0.15`  | Returns `503` with an empty body           |
| Slow rate        | `CQAPI_SLOW_RATE`      | `0.10`  | Sleeps `CQAPI_SLOW_MS` to force a timeout  |
| Slow duration    | `CQAPI_SLOW_MS`        | `3000`  | Exceeds the Middleware per attempt timeout |
| Normal latency   | `CQAPI_LATENCY_MIN_MS` | `50`    | Uniform lower bound                        |
|                  | `CQAPI_LATENCY_MAX_MS` | `400`   | Uniform upper bound                        |
| Determinism      | `CQAPI_RANDOM_SEED`    | `0`     | `0` means time seeded                      |

`503` is chosen over `500` deliberately. See the retry rule in section 6.

### api-key enforcement

Missing key and wrong key both return `401` with an empty body. No `WWW-Authenticate`, no hint as to
which failed. Comparison is constant time.

## 3. Middleware contract

`POST /api/v1/quotes`. Published in `api/openapi.yaml`.

Same request and response bodies as the vendor. The MVP contract deliberately mirrors the vendor;
divergence is expected later and is the reason the layer exists.

Headers: `Authorization: Bearer <jwt>` (required), `X-Correlation-Id`, `traceparent`.

## 4. Validation

Authoritative in the Middleware. Mirrored in the FE for feedback only. The FE is never trusted.

| Field              | Rule                                              | Code                 |
|--------------------|---------------------------------------------------|----------------------|
| `loanAmount`       | required, number, 1000.00 to 5000000.00 inclusive | `amount_out_of_range`|
| `loanAmount`       | at most 2 decimal places                          | `amount_precision`   |
| `loanTermInMonths` | required, integer, 6 to 360 inclusive             | `term_out_of_range`  |
| `loanTermInMonths` | no fractional part                                | `term_not_integer`   |
| `riskBand`         | required, one of `A`, `B`, `C`                    | `risk_band_invalid`  |
| body               | valid JSON, no unknown fields                     | `malformed_body`     |

All failures are collected and returned together, not first failure wins.

Edge cases the tests must cover: `loanAmount` as a string, negative, zero, `1e9` notation,
`999.999`; `loanTermInMonths` as `12.5`, `0`, `361`; `riskBand` as `"b"`, `"D"`, empty; empty body;
unknown field.

## 5. Error taxonomy

Single response shape at every layer:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "Check the highlighted fields.",
    "details": [{ "field": "loanAmount", "code": "amount_out_of_range" }],
    "correlationId": "4b1c..."
  }
}
```

`message` is always safe to render to a staff user. `details` is present only for validation.

| Failure class                     | Middleware        | Code                      | User message                                          |
|-----------------------------------|-------------------|---------------------------|-------------------------------------------------------|
| Validation failed                 | `400`             | `VALIDATION_FAILED`       | Check the highlighted fields.                          |
| Missing or invalid bearer         | `401`             | `UNAUTHENTICATED`         | Your session has expired. Sign in again.               |
| Valid caller, missing scope       | `403`             | `FORBIDDEN`               | You do not have access to generate quotes.             |
| Vendor rejected our `api-key`     | `502`             | `UPSTREAM_UNAVAILABLE`    | Quotes are unavailable right now. Try again shortly.   |
| Vendor 5xx, retries exhausted     | `502`             | `UPSTREAM_UNAVAILABLE`    | Quotes are unavailable right now. Try again shortly.   |
| Vendor response unparseable       | `502`             | `UPSTREAM_CONTRACT`       | Quotes are unavailable right now. Try again shortly.   |
| Timeout or total budget exceeded  | `504`             | `UPSTREAM_TIMEOUT`        | The quote service took too long. Try again.            |
| Circuit breaker open              | `503` + `Retry-After` | `UPSTREAM_CIRCUIT_OPEN` | Quotes are paused briefly. Try again in a moment.      |
| Panic or unexpected failure       | `500`             | `INTERNAL`                | Something went wrong. Try again.                       |

The vendor `api-key` failure maps to `502`, never `401`. A vendor credential problem is our
operational fault, not the staff user's authentication problem, and conflating them would both
mislead the user and leak that a credential exists. The real cause is logged at `error` with the key
masked. This implements `assumptions.md` API Key Handling item 4.

`INTERNAL` was added during CQ-02. Panic recovery has to render something, and without a named class
it would have invented an envelope outside this table. Any error that is not one of the classes above
is rendered as `INTERNAL`, so an unexpected error can never leak its text to a caller.

BFF behaviour: pass the Middleware status and body through unchanged, except `401`, which it maps to
its own session semantics and which triggers a sign in redirect in the FE.

## 6. Resilience budgets

Referenced by `assumptions.md` Non-Functional Requirements 1.2.

| Hop                  | Per attempt | Total budget | Attempts        |
|----------------------|-------------|--------------|-----------------|
| Middleware to CQAPI  | 2s          | 6s           | 3 (1 + 2 retry) |
| BFF to Middleware    | 8s          | 8s           | 1 (no retry)    |
| FE to BFF            | 10s abort   | 10s          | 1 (no retry)    |

Budgets nest: each caller's timeout exceeds the callee's total, so the inner layer always reports
the specific failure rather than the outer layer reporting a generic timeout.

Backoff: base 150ms, factor 2, full jitter, cap 1s. Sleep is skipped if the remaining total budget
is less than the next delay.

Circuit breaker (Middleware to CQAPI): rolling window 20 requests, minimum 10 samples, trips at 50%
failures, open 10s, half open allows 3 probes.

### Retry rule

`assumptions.md` assumes quote generation is not idempotent at the vendor, so a retry may create a
second quote. Retries are therefore restricted to failures that provably produced no quote.

| Condition                                    | Retry | Reason                                     |
|----------------------------------------------|-------|--------------------------------------------|
| Connection refused, DNS failure, TCP reset   | yes   | Request never reached the application      |
| Timeout before response headers              | yes   | No response was begun                      |
| `502`, `503`, `504`                          | yes   | Rejected at the edge, handler did not run  |
| `429`                                        | yes   | Honour `Retry-After` if present            |
| `500`                                        | no    | Ambiguous, the vendor may have created it  |
| Other `4xx`                                  | no    | Deterministic, a retry repeats the result  |
| `2xx` with an unparseable body               | no    | A quote likely exists                      |

`500` not being retryable is the direct consequence of non idempotency. This is why the CQAPI mock
injects `503` rather than `500`; `503` is the honest signal for a request that was never processed.

Per `assumptions.md` 1.5, a quote generated at the vendor but not received is abandoned, not
reconciled. Quotes are advisory and not binding.

## 7. Authentication

Two separate concerns, never conflated.

### Staff to BFF

Cookie `cq_session`, `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`. Value is an opaque random 256
bit identifier. `AuthProvider` resolves it against an in memory store seeded with one fake staff
user. `POST /api/session` signs in, `DELETE /api/session` signs out, `GET /api/session` returns the
current user or `401`.

### BFF to Middleware

Signed JWT, HS256, shared secret `BFF_MIDDLEWARE_SIGNING_KEY`.

| Claim   | Value                     |
|---------|---------------------------|
| `iss`   | `cqapp-bff`               |
| `aud`   | `cqapp-middleware`        |
| `sub`   | staff identifier          |
| `scope` | `["quote:generate"]`      |
| `exp`   | issued time + 60s         |
| `iat`   | issued time               |
| `jti`   | UUIDv4                    |

The Middleware verifies signature, `iss`, `aud`, `exp`, and requires the `quote:generate` scope.
Short expiry because the token is minted per request and never leaves the mesh.

Production replacement, documented not built: the IdP issues the staff token, the BFF validates it,
and the Middleware validates the same token or a mesh issued workload identity. The shared secret
disappears.

## 8. Observability

| Concern        | Decision                                                                 |
|----------------|--------------------------------------------------------------------------|
| Correlation id | Header `X-Correlation-Id`. Precedence: valid inbound header, then the active trace id so logs and traces join on one value, then a fresh random 128 bit id. |
| Trace context  | W3C `traceparent`, propagated on every hop, spans on inbound and outbound |
| Log format     | JSON, one line per event                                                  |
| Log fields     | `ts`, `level`, `msg`, `component`, `method`, `line`, `correlationId`, `traceId`, `spanId` |
| Echoed         | `correlationId` appears in every error body so a user can quote it        |

Never logged: the `api-key` value, bearer tokens, cookie values. The key is logged masked as
`****<last 4>` so an operator can identify which key is loaded without recovering it.

Logged: `loanAmount`, `loanTermInMonths`, `riskBand`. Business data, not PII, per `assumptions.md`
2.2.

An inbound `X-Correlation-Id` is attacker controlled, so it is accepted only when it matches
`[A-Za-z0-9._-]{1,64}`. A value outside that is discarded and replaced, not trimmed: a truncated id
would silently break the join across services. This keeps hostile content out of log lines and out of
the response header.

No collector is deployed. `OTEL_EXPORTER_OTLP_ENDPOINT` unset means spans are created and propagated
but not exported. When it is set, it is validated at startup, because the OTLP exporter otherwise
accepts a malformed endpoint and silently drops every trace.

## 9. Configuration

| Env var                      | Components            | Default                    | Required |
|------------------------------|-----------------------|----------------------------|----------|
| `CQAPI_API_KEY`              | middleware, cqapi     | none                       | yes      |
| `CQAPI_BASE_URL`             | middleware            | `http://cqapi:8083`        | no       |
| `MIDDLEWARE_BASE_URL`        | bff                   | `http://middleware:8082`   | no       |
| `BFF_MIDDLEWARE_SIGNING_KEY` | bff, middleware       | none                       | yes      |
| `PORT`                       | all                   | per component below        | no       |
| `LOG_LEVEL`                  | all                   | `info`                     | no       |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| all                   | unset                      | no       |

Startup fails fast and loudly if a required secret is absent.

| Component  | Port   | Reachable from the browser |
|------------|--------|----------------------------|
| edge       | `8080` | yes, the only origin       |
| bff        | `8081` | no                         |
| middleware | `8082` | no                         |
| cqapi      | `8083` | no                         |

## 10. Testing strategy

`assumptions.md` does not mention testing; the challenge grades it. Filling that gap here.

| Layer      | Approach                                                                   |
|------------|----------------------------------------------------------------------------|
| Go units   | Table driven, `httptest` fakes, `-race`, no network                        |
| Validation | One table covering every edge case listed in section 4                     |
| Resilience | Fake vendor: timeout, retry then succeed, no retry on `500`, breaker cycle |
| Contract   | Middleware responses asserted against `api/openapi.yaml` shapes            |
| FE         | Vitest and React Testing Library on validation and state rendering         |

Determinism: `CQAPI_RANDOM_SEED` for injection, injected clocks for backoff and breaker timing.

Not built, documented only: browser end to end tests, load tests, contract tests against the real
vendor.
