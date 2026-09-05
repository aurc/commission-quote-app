# CQ-04 Middleware core and OpenAPI

## Context

The Middleware is the only component that holds the vendor credential and the only one that decides
whether a caller may generate a quote. Everything the browser eventually reaches passes through here.

This task builds the request path: verify the caller's claims, validate authoritatively, call the
vendor once, translate the vendor's world into ours. Retries, backoff and the circuit breaker are
CQ-05, so a single attempt with a hard timeout is the boundary of this PR.

Contract in `design/contract.md` sections 3 to 7. Published as `api/middleware.openapi.yaml`.

## Deliverables

```
api/middleware.openapi.yaml   our published contract
cmd/cqapp-middleware/main.go
internal/middleware/
  auth.go        bearer verification, algorithm pinning
  entitle.go     Entitlements port and the in memory grant table
  validate.go    authoritative validation
  vendor.go      CQAPI client, api-key injection, error translation
  handler.go     POST /v1/quotes
  config.go
```

`internal/platform/money` and the design realignment landed first, in their own commit on this
branch.

## Design

### Two things to settle before code

**JWT verification uses `github.com/golang-jwt/jwt/v5`, not hand rolled.**
CQ-03 hand rolled a UUID because ten lines of bit twiddling did not justify a dependency. This is the
opposite case. HS256 verification is short but unforgiving: forget to reject `alg: none`, compare the
MAC with `==`, or skip the `aud` check, and the result still passes every happy path test while being
trivially forgeable. A well known library is the responsible choice, and the distinction between the
two cases is worth being explicit about.

**Money moved to `internal/platform/money`, on `math/big.Rat`.**
Done ahead of the handler work. Amounts and rates are exact rationals, nothing rounds until a value
reaches a boundary, and boundaries are `int64` cents or ten thousandths. The Middleware needs the
same parsing the vendor uses, and importing the vendor mock into our own service would couple us to
a component meant to be deleted when the real vendor arrives.

### Claim verification and entitlement

Per `contract.md` section 7, which was rewritten for this task after the scope check turned out to be
circular.

Verify the signature, `iss`, `aud` and `exp`, with `alg` pinned to HS256 so a token cannot choose its
own verification. Then, separately, decide entitlement.

The `scope` claim is the caller asking to do something, not a grant. The BFF writes it and the BFF is
the party being checked, so trusting it would mean the BFF decides its own authority and the `403`
branch could never be reached. The Middleware owns the decision through an `Entitlements` port,
seeded in the MVP with one entitled and one unentitled staff member so `403` is a real path.

- missing, malformed, expired, badly signed, wrong `alg`, `iss` or `aud`: `401 UNAUTHENTICATED`
- valid token, scope not requested, or requested but not granted: `403 FORBIDDEN`

That split matters. A `401` tells the front end to send the user back to sign in; a `403` says
signing in again will not help.

Small clock skew leeway, since `exp` is 60 seconds and two containers need not agree to the
millisecond. Configurable, defaulting to a few seconds.

The published spec declares the scope each operation requires and documents `403`, so a consumer
learns the requirement from the contract rather than by being refused.

### Validation

Authoritative here, per `contract.md` section 4. Every rule, every failure collected and returned
together rather than first failure wins, because a form that reveals one error at a time is a poor
experience and the front end mirrors this list.

Same raw JSON text approach as CQ-03: `999.999` has to be distinguishable from `1000.00`, and a
quoted `"1000"` has to be rejected. That check earned its place in CQ-03 and is not weakened here.

### Vendor call

A client that attaches `api-key` from the `SecretProvider`, propagates trace context, and bounds a
single attempt with a timeout. CQ-05 wraps this; nothing here anticipates that beyond keeping the
call in one place.

The response is decoded, checked against the vendor contract, and re-encoded into ours. Not streamed
through: we publish a contract and should be able to state that what we return matches it.

**The commission is never recomputed.** The vendor owns the formula, and a Middleware that recomputes
would silently disagree with the vendor the day their pricing changes. A test proves this by having a
fake vendor return a commission that does not match the formula and asserting we pass it through
unchanged.

### Error translation

| Vendor result | Ours | Why |
|---|---|---|
| `201`, parseable | `200` | We create nothing, so we do not claim `201` |
| `201`, unparseable | `502 UPSTREAM_CONTRACT` | They broke the contract |
| `401` | `502 UPSTREAM_UNAVAILABLE` | Our credential problem, never the user's `401` |
| `400` | `502 UPSTREAM_CONTRACT` | Our validation and theirs have diverged. Log loudly |
| `5xx` | `502 UPSTREAM_UNAVAILABLE` | |
| timeout | `504 UPSTREAM_TIMEOUT` | |

The vendor `400` case is the interesting one. We validated and passed; they rejected. That is a
contract drift bug on our side, not a user error, so it must not come back as a `400` to the caller.

## Tests

| Area | Cases |
|---|---|
| Claims | No header, wrong scheme, bad signature, `alg: none`, `alg` swapped, expired, not yet valid, wrong `iss`, wrong `aud`, valid |
| Entitlement | Scope requested but not granted is `403`; granted proceeds; a forged scope claim does not grant itself access |
| Validation | Every edge case in `contract.md` section 4, plus all failures returned together |
| Boundaries | Amount and term exactly at each limit, inside and outside |
| Translation | The table above, against a fake vendor |
| Passthrough | A vendor commission inconsistent with the formula is returned unchanged |
| Leakage | No `api-key`, bearer token or vendor URL in any response body |
| Spec | Documented statuses match what the handler emits, required fields enforced, ranges match the validator, declared scope matches the one enforced |

## Verification

`make check` green. Manually against the real CQAPI from CQ-03: a valid request returns a quote; a
wrong `CQAPI_API_KEY` returns `502` with a message that does not mention credentials, while the log
records the real cause with the key masked.
