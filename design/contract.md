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

AUD only. On the wire, amounts are JSON numbers with exactly 2 decimal places and rates JSON numbers
with exactly 4 (`0.0125` is 1.25%). Input amounts are accepted with at most 2 decimal places.

**Arithmetic is exact, never floating point.** `internal/platform/money` holds amounts and rates as
`math/big.Rat`. A decimal is a ratio of integers, so a `Rat` represents `0.01` and `250000.00 *
0.0180` exactly, where `float64` cannot: it gives `4500.000000000001`, and errors of that shape
accumulate into a number somebody is eventually paid.

Nothing rounds mid calculation. A value is held at full precision until it reaches a boundary, and
rounding is then one named operation, `RoundHalfUp`, applied once. Half up means a tie goes up, so
`10.005` renders as `10.01`. Amounts are validated positive before any rounding, so the behaviour of
a negative tie never arises.

**Boundaries are integers.** Where a value leaves the exact world for JSON, a log line or a store, it
crosses as whole cents (`int64`) or whole ten thousandths (`int64`), never as a float. `Cents` and
`TenThousandths` report whether the value fits, because an amount arrives from the network before it
has been range checked and could be arbitrarily large. Overflow is reported, never wrapped.

Rate cards are integers by nature. `0.0150` is 150 ten thousandths, a whole number of basis points,
not a measured quantity, so pricing tables are written as integers and converted.

## 2. Vendor contract (CQAPI)

`POST /v1/quotes`. Published in `api/cqapi.openapi.yaml`.

Headers: `api-key` (required), `Content-Type: application/json`, `X-Correlation-Id`, `traceparent`.

Request:
```json
{ "loanAmount": 250000.00, "loanTermInMonths": 240, "riskBand": "B" }
```

Response `201`:
```json
{ "quoteId": "b3f1c9e2-...", "commissionRate": 0.0165, "totalCommission": 4125.00 }
```

`quoteId` is a UUIDv4 generated per request.

`201` rather than `200` is deliberate, and is the one place the two contracts differ. The vendor is
modelled as a system that records the quote it issues: that is exactly why `assumptions.md` 1.4
assumes generation is not idempotent there, and why a retry risks a duplicate. A system that creates
something returns `201`. Our own Middleware creates nothing and returns `200`; see section 3.

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

`POST /v1/quotes`, returning `200`. Published in `api/middleware.openapi.yaml`.

Same request and response bodies as the vendor. The MVP contract deliberately mirrors the vendor;
divergence is expected later and is the reason the layer exists.

Headers: `Authorization: Bearer <jwt>` (required), `X-Correlation-Id`, `traceparent`.

Two deliberate differences from the vendor contract:

**The path carries no `/api` prefix.** `/api` is the Edge's routing concern, the segment it uses to
decide browser traffic goes to the BFF rather than to static assets. An internal service that is
never browser facing should not carry a routing prefix that belongs to a layer two hops away. The
browser calls `/api/v1/quotes`, the Edge forwards to the BFF, and the BFF calls the Middleware at
`/v1/quotes`.

**The status is `200`, not `201`.** We create nothing. `assumptions.md` 1.5 and 1.7 state the app is
stateless with no quote lifecycle, so there is no resource to have created, nothing to put in a
`Location` header, and nothing to retrieve afterwards. Returning `201` would claim otherwise. The
Middleware translating the vendor's `201` into our `200` is a small, concrete example of the layer
doing its job rather than forwarding bytes.

## 4. Validation

Authoritative in the Middleware. Mirrored in the FE for feedback only. The FE is never trusted.

| Field              | Rule                                              | Code                 |
|--------------------|---------------------------------------------------|----------------------|
| `loanAmount`       | present, and an unquoted JSON number               | `amount_invalid`     |
| `loanAmount`       | at most 2 decimal places                          | `amount_precision`   |
| `loanAmount`       | 1000.00 to 5000000.00 inclusive                    | `amount_out_of_range`|
| `loanTermInMonths` | present, and an unquoted JSON number               | `term_invalid`       |
| `loanTermInMonths` | no fractional part                                | `term_not_integer`   |
| `loanTermInMonths` | 6 to 360 inclusive                                 | `term_out_of_range`  |
| `riskBand`         | required, one of `A`, `B`, `C`                    | `risk_band_invalid`  |
| body               | valid JSON object, no unknown fields               | `malformed_body`     |

`amount_invalid` and `term_invalid` are separate from the range codes because a
missing field and a quoted `"1000"` are not out of range, and the front end maps
each code to its own wording. One rule per code.

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
| Vendor rejected our request `400` | `502`             | `UPSTREAM_CONTRACT`       | Quotes are unavailable right now. Try again shortly.   |
| Timeout or total budget exceeded  | `504`             | `UPSTREAM_TIMEOUT`        | The quote service took too long. Try again.            |
| Circuit breaker open              | `503` + `Retry-After` | `UPSTREAM_CIRCUIT_OPEN` | Quotes are paused briefly. Try again in a moment.      |
| Panic or unexpected failure       | `500`             | `INTERNAL`                | Something went wrong. Try again.                       |

The vendor `api-key` failure maps to `502`, never `401`. A vendor credential problem is our
operational fault, not the staff user's authentication problem, and conflating them would both
mislead the user and leak that a credential exists. The real cause is logged at `error` with the key
masked. This implements `assumptions.md` API Key Handling item 4.

A vendor `400` is not passed through as a `400`. We validated the request and accepted it, and they
rejected it, which means our validation and theirs have drifted apart. That is our bug, not the
user's mistake, so it must not be reported to the caller as though they got something wrong. It is
logged at error level because it means the contract needs attention.

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

The Middleware verifies the signature, `iss`, `aud` and `exp`, accepting exactly one algorithm.
`alg` is pinned to HS256 and every other value is rejected, `none` included: a verifier that trusts
the header's choice of algorithm can be handed an unsigned token, or an HMAC token verified with a
public key it thought was for RSA.

Short expiry because the token is minted per request and never leaves the mesh. A few seconds of
clock skew leeway is allowed, since two containers need not agree to the millisecond.

### Authorisation is not the same as authentication

A verified signature proves the token came from a holder of the secret. It does not establish that
the subject may generate quotes.

The `scope` claim is the caller **asking** to do something. It is not a grant. The BFF writes it, and
the BFF is the party being checked, so treating it as a grant would make the check circular: the BFF
would decide its own authority and the Middleware would confirm the BFF's opinion of itself. The
`403` branch could never be reached.

The Middleware therefore owns the decision, through an `Entitlements` port alongside the existing
`AuthProvider` and `SecretProvider` seams:

```go
type Entitlements interface {
    Granted(ctx context.Context, subject string) ([]string, error)
}
```

A request is authorised only when **both** hold:

1. the token is valid and requests `quote:generate` in its `scope` claim, and
2. the Middleware's own `Entitlements` source grants `quote:generate` to that `sub`.

| Outcome | Response |
|---|---|
| No token, malformed, expired, bad signature, wrong `iss` or `aud`, wrong `alg` | `401 UNAUTHENTICATED` |
| Valid token, scope not requested | `403 FORBIDDEN` |
| Valid token, scope requested, subject not granted it | `403 FORBIDDEN` |
| Valid token, scope requested and granted | proceed |

MVP implementation: an in memory grant table seeded with one entitled staff member and one
unentitled one, so the `403` path is real and testable rather than theoretical. This satisfies
`assumptions.md` FR4, which assumes pre configured access without saying who holds it.

Production: the same interface backed by group membership from the directory, or a policy decision
point. The interface is the seam; the source changes, the Middleware does not.

### Required scopes are published

The Middleware's OpenAPI file states the scope each operation requires, so a consumer learns it from
the contract rather than from a `403`. `POST /v1/quotes` requires `quote:generate`, declared on the
operation and described on the security scheme, and `403` is a documented response.

The scheme is `http` `bearer` with `bearerFormat: JWT`, because that is what the transport actually
is. An `oauth2` scheme would carry scopes natively, but it would also have to name a token endpoint,
and there is no such endpoint in the MVP: the BFF mints the token itself. Inventing a `tokenUrl` to
satisfy the schema would put a fiction in a published contract. When the IdP arrives the scheme
becomes `openIdConnect` against real discovery, and the declared scopes carry over unchanged.

### Stated limitations

**HS256 is symmetric, so there is no non repudiation.** The Middleware can prove a token was minted
by a holder of `BFF_MIDDLEWARE_SIGNING_KEY`. It cannot prove the holder was the BFF. Any component
with the secret, the Middleware included, can mint a token the Middleware will accept. This is
acceptable only because the secret lives in one place and never leaves the mesh.

Production removes the property rather than mitigating it: the BFF signs with a private key and the
Middleware holds only the public half, or both validate an IdP issued token against JWKS. Either way
the Middleware loses the ability to mint what it verifies.

**`jti` is carried but not enforced.** It is logged for correlation and is the hook a replay cache
would use. With a 60 second expiry on a token that never leaves the mesh, a replay window that small
did not justify the state in the MVP. Tracked as depth, not forgotten.

**The staff session itself is fake.** `AuthProvider` returns a seeded in memory user. Everything
above describes how the Middleware treats the claims it is given, not how those claims come to be
trustworthy in the first place; that is the IdP's job and is documented, not built.

## 8. Observability

| Concern        | Decision                                                                 |
|----------------|--------------------------------------------------------------------------|
| Correlation id | Header `X-Correlation-Id`. Precedence: valid inbound header, then the active trace id so logs and traces join on one value, then a fresh random 128 bit id. |
| Trace context  | W3C `traceparent`, propagated on every hop, spans on inbound and outbound |
| Log format     | JSON, one line per event                                                  |
| Log fields     | `ts`, `level`, `msg`, `component`, `method`, `line`, `correlationId`, `traceId`, `spanId` |
| Attribution    | Quote requests also log `staffId`, the token's `sub`, and `jti`. An action a bank cares about should be attributable to someone |
| Echoed         | `correlationId` appears in every error body so a user can quote it        |

Never logged: the `api-key` value, bearer tokens, cookie values. The key is logged masked as
`****<last 4>` so an operator can identify which key is loaded without recovering it.

Logged: `loanAmount`, `loanTermInMonths`, `riskBand`. Business data, not PII, per `assumptions.md`
2.2. Also `staffId`, which is an internal subject identifier, never a name or an email address.

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

## 10. Front end contract

The brief specifies parts of the UI literally, so they are pinned here rather than left to CQ-07.

| Element | Requirement |
|---|---|
| Form fields | `loanAmount`, `loanTermInMonths`, `riskBand` |
| Submit control | A button labelled **Generate Quote** |
| Success | A display area showing `quoteId`, `commissionRate` and `totalCommission` |
| In flight | A visible loading state; the submit control is disabled while a request is open |
| Failure | The `message` from the error envelope, shown as an error, with `correlationId` available |
| Field errors | `details` mapped back to the field that failed, shown inline |

Presentation: `commissionRate` is shown as a percentage to two decimal places (`0.0180` renders as
`1.80%`), `totalCommission` as AUD currency. The FE formats for display only; it never recomputes a
figure the vendor produced.

Validation in the FE mirrors section 4 for immediate feedback and is never trusted. The Middleware
rejects independently, and the FE renders whatever it sends back.

## 11. Testing strategy

`assumptions.md` does not mention testing; the challenge grades it. Filling that gap here.

| Layer      | Approach                                                                   |
|------------|----------------------------------------------------------------------------|
| Go units   | Table driven, `httptest` fakes, `-race`, no network                        |
| Validation | One table covering every edge case listed in section 4                     |
| Resilience | Fake vendor: timeout, retry then succeed, no retry on `500`, breaker cycle |
| Contract   | Each service's responses asserted against its own published OpenAPI file   |
| FE         | Vitest and React Testing Library on validation and state rendering         |

Determinism: `CQAPI_RANDOM_SEED` for injection, injected clocks for backoff and breaker timing.

Not built, documented only: browser end to end tests, load tests, contract tests against the real
vendor.
