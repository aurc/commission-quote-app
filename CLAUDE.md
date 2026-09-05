# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Hard rules

- **Never merge a PR.** The repo owner reviews and merges in GitHub. Open PRs, never merge them.
- **Never push to `main`.** Work happens on `cq-0N-<slug>` branches.
- **Pause after the plan.** Commit the task plan, then stop and let the owner review it before
  writing any implementation. Do not run the plan commit and the implementation in one go.
- **Run `make check` before every commit.** Do not commit with a failing test.
- Prose style in docs and replies: concise, tables over paragraphs, no em dashes.

## Commands

Go 1.26, single module. `golangci-lint` is used if installed and skipped if not.

| Command | Does |
|---|---|
| `make check` | fmt, vet, `go test -race ./...`. Run before committing |
| `make test` | Tests with the race detector |
| `go test ./internal/cqappmiddleware/ -run TestAuthenticationFailuresAre401` | A single test |
| `make cover` | Coverage per package |
| `make env` | Create `.env` from `.env.example`, needed before any `run-` or `dev-` target |
| `make run-cqapi-mock`, `make run-cqapp-middleware`, `make run-cqapp-bff` | Run one service natively |
| `make dev-token` | Development bearer token, for calling the Middleware without the BFF |
| `make dev-staff ARGS='-id staff-004 -name "Jane Doe"'` | Add a staff member, prompting for a password |
| `make build` | Build into `bin/` |

Make targets are named after what they act on, so `cmd/cqapp-bff` is `make run-cqapp-bff`, and
development only tools carry a `dev-` prefix.

`.env` loads only into `run-` and `dev-` targets, never into `make test`. Tests must not depend on a
developer's environment; keep them hermetic in their own right, not because the Makefile is careful.

Ports: edge 8080, bff 8081, middleware 8082, cqapi-mock 8083.

## Current state

| Done | Remaining |
|---|---|
| CQ-01 to CQ-06: design, platform, vendor mock, middleware, resilience, BFF | CQ-07 web, CQ-08 edge and compose |

Authoritative documents, read both before writing code:

| File | Holds |
|---|---|
| `design/assumptions.md` | Why: posture, trade-offs, scope, delivery tiers |
| `design/contract.md` | What: schemas, validation rules, commission formula, error taxonomy, resilience budgets, auth claims, config, testing strategy |
| `tasks/register.md` | The 8 task register and per task workflow |
| `source/challenge.md`, `source/*.pdf` | The original brief |

`contract.md` wins on behaviour. `assumptions.md` wins on intent.

## What is being built

Staff facing web app: capture `loanAmount`, `loanTermInMonths`, `riskBand`, return a commission
quote (`quoteId`, `commissionRate`, `totalCommission`) from a mocked vendor API.

4 hour timebox. Readable code, core tests, clear edge case handling, simple run instructions. The
codebase gets extended live in a follow up session, so structure matters more than completeness.

## Architecture

Five components, single browser visible origin. Do not short circuit the layering: the FE never
calls the Middleware, and the vendor key never reaches the BFF or the browser.

| Component | Stack | Port | Owns |
|---|---|---|---|
| Edge | nginx | 8080 | Single origin, TLS, FE assets with SPA fallback, `/api` to BFF |
| FE | React + Vite | via edge | Form, inline validation, loading/error/result states |
| BFF | Go | 8081 | Password sign in, session cookie, cookie to bearer exchange, user facing wording. No business logic, no vendor credential |
| Middleware | Go | 8082 | Claim verification, authoritative validation, retries, breaker, OpenAPI, holds the `api-key` |
| cqapi-mock | Go | 8083 | Vendor contract, `api-key` enforcement, failure and latency injection |

Abbreviations: **CQApp** (this app), **CQAPI** (mocked vendor), **FE**, **BFF**.

## Layout

```
go.mod                    single module
api/
  cqapi.openapi.yaml      vendor published contract, hand written
  middleware.openapi.yaml our published contract, hand written
cmd/
  cqapp-bff/       our binaries carry the cqapp- prefix
  cqapp-middleware/
  cqapi-mock/      the vendor stand in, named for what it is
internal/
  platform/               log, config, otel, secrets, http helpers
  cqappbff/  cqappmiddleware/  cqapimock/
web/                      React + Vite
deploy/                   nginx.conf, docker-compose.yml, Dockerfiles
```

Reviewer run path is `docker compose up`. Makefile targets exist for native dev.

## Constraints that are easy to get wrong

- **Vendor `api-key` is Middleware only.** Env `CQAPI_API_KEY` via `SecretProvider`. Never in FE
  bundles, never in a BFF response, never past the browser. Masked in logs as `****<last 4>`.
- **A vendor auth failure maps to `502`, never `401`.** Staff auth and vendor auth are separate
  concerns. Log the real cause, return a non revealing message.
- **A vendor `500` is not retryable.** Quote generation is non idempotent, so `500` is ambiguous.
  Retry only connection failures, pre header timeouts, `502`/`503`/`504`, `429`. Full table in
  `contract.md` section 6.
- **The vendor owns the commission formula.** The Middleware passes the result through and never
  recomputes it.
- **Validation is authoritative in the Middleware.** The FE mirrors it for feedback only.
- Stateless everywhere. No persistence. Quotes are advisory, not binding.
- `loanAmount` is logged as business data. Secrets, bearer tokens and cookie values never are.
- Single currency AUD, no localisation. View only staff user.
- **Money is `math/big.Rat`, never `float64`.** `internal/platform/money`. Nothing rounds until a
  boundary, where `RoundHalfUp` is applied once. Boundaries are `int64` cents or ten thousandths and
  report overflow rather than wrapping.
- **Credentials live in `config/credentials.csv`, never in `staff.csv`.** The Middleware reads
  identity and entitlement and must never load password material. bcrypt, never plaintext, in the
  fixture too. Sign in failures are uniform so timing does not reveal who exists.
- **Staff live in `config/staff.csv`, never hard coded.** Read through `internal/platform/staffdir`.
  The BFF takes identity from it and the Middleware takes entitlement, so the two cannot disagree
  about who exists. Keep at least one member with no scopes or the `403` path stops being tested.
- **A token's `scope` claim is a request, not a grant.** The BFF writes it and the BFF is the party
  being checked, so trusting it alone is circular. The Middleware decides entitlement from its own
  `Entitlements` source. Both conditions are required.
- **The Middleware writes API messages, the BFF writes user copy.** No "sign in again" in a service
  with no browser. `code` is the stable contract; prose is presentation. A test enforces this.
- **No Go service terminates TLS.** Public TLS is the Edge's, service to service is the mesh
  sidecar's mTLS. Outbound to the vendor is ours: the Middleware refuses to start on plain HTTP to a
  non local host, because the `api-key` would cross the network in clear.
- **Retry only what provably produced no quote**, plus timeouts, which `assumptions.md` 1.5 already
  accepts regenerating. Never a `500`. The breaker sits outside the retrier.
- **Numbers arrive as raw JSON text, not decoded floats.** It is the only way to tell `999.999` from
  `1000.00`, and it is what lets a quoted `"1000"` be rejected.

## Workflow

Per task, per `README.md`:

1. Branch `cq-0N-<slug>`.
2. Commit 1: the planned work. Plan at `tasks/CQ-0N/plan.md`, register entry set to `wip`.
3. **Stop here.** The owner reviews the plan before implementation starts.
4. Commit 2: the finished implementation and its tests, register entry set to `done`.
5. Open a PR titled `CQ-0N - <title>`. Do not merge it.

`README.md` must end up with an AI usage transparency section, required by the brief.

## Verifying by hand

Kill stale processes **by port**, not by name: `go run` children have survived `pkill -f` twice in
this repo and served requests that looked like passing checks.

```sh
for p in 8081 8082 8083; do
  PIDS=$(lsof -nP -tiTCP:$p -sTCP:LISTEN); [ -n "$PIDS" ] && kill -9 $PIDS
done
```

`httpx.Serve` binds before it logs `listening`, so a failed bind never reads as a running service.
If a manual check gives a surprising result, confirm which process actually answered.
