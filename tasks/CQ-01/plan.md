# Commission Quote App — Task Register & Delivery Plan

## Context

The repository is documentation-only: `README.md`, `design/assumptions.md`, `design/design.svg`, the
original brief in `source/`, and an empty `tasks/register.md`. No code, no `go.mod`, no
`package.json`.

`README.md` commits to a transparent, AI-first workflow: work is split into tasks held in a
register, each task has a `code`, each task gets a planning commit and a finished-work commit, and
each task lands as a PR whose title starts with that code. This plan populates that register and
fixes the sequencing so the work can start.

Before any code is written, `design/assumptions.md` gets a completeness pass. It is a strong
statement of posture and trade-offs, but it is not yet sufficient to implement against — it omits
the commission formula, the `riskBand` domain, validation rules, an error taxonomy, and a testing
strategy, and it contains two internal inconsistencies (see CQ-01). Closing those gaps first is
cheaper than discovering them mid-implementation, and it makes every later PR a mechanical
translation of an agreed contract.

Intended outcome: a reviewer clones the repo, runs one command, gets a working staff-facing quote
app; and the commit and PR history reads as a deliberate, documented sequence of decisions.

## Decisions taken

| Decision     | Choice                                                                                    |
|--------------|-------------------------------------------------------------------------------------------|
| Go layout    | Single module at root, `cmd/<binary>`, shared code in `internal/platform`                 |
| Run story    | `docker compose up` as the reviewer path; Makefile targets for native dev                 |
| OpenAPI      | Hand-written `api/openapi.yaml` as the published contract, hand-written handlers          |
| Design docs  | Repair `design/assumptions.md`; add `design/contract.md` for testable detail              |
| Scope stance | Tier every scope item (core / depth / documented-only); recommend, never cut unilaterally |
| Granularity  | 8 tasks, one PR each                                                                      |

## Target layout

```
go.mod
api/openapi.yaml          Middleware published contract
cmd/{bff,middleware,cqapi}/main.go
internal/
  platform/               log, config, otel, secrets, http helpers
  bff/  middleware/  cqapi/
web/                      React + Vite SPA
deploy/                   nginx.conf, docker-compose.yml, Dockerfiles
design/  tasks/  source/
```

Ports (proposed, confirmed in CQ-01): edge `8080`, bff `8081`, middleware `8082`, cqapi `8083`.

## Task register

Written to `tasks/register.md` as part of CQ-01. PR titles: `CQ-0N — <title>`.

### CQ-01 — Design completeness pass

Docs only, no code. Deliverables: repaired `design/assumptions.md`, new `design/contract.md`,
populated `tasks/register.md`.

Gaps to close, each already identified by reading `assumptions.md` against the brief:

| Gap                                                                         | Why it blocks implementation                         |
|-----------------------------------------------------------------------------|------------------------------------------------------|
| `assumptions.md` cites a *Resilience Budgets* section that does not exist   | Retry/timeout/breaker values are undefined           |
| Scope table promises a "production step" column; only the MVP column exists | The build/document split is unstated                 |
| No commission formula                                                       | CQAPI cannot be written                              |
| No `riskBand` domain                                                        | No FE control, no validation on either side          |
| No validation rules (amount range/precision, term bounds)                   | The brief grades invalid-number handling             |
| No error taxonomy or error response shape                                   | Every layer's failure path is guesswork              |
| BFF→Middleware bearer exchange unspecified                                  | Token contents and Middleware verification undefined |
| Correlation-id header name and OTel trace-id relationship                   | Cross-service propagation cannot be implemented      |
| No testing strategy                                                         | "Include core tests" is a graded must-have           |
| CQAPI failure/latency injection rates, env var names, ports                 | Needed for compose and deterministic tests           |
| "Retry only where no quote was returned" not operationalised                | Needs an explicit retryable-condition rule           |

`contract.md` carries: request/response schemas, validation rules, the commission formula, the
error taxonomy with failure-class → status → user message mapping, resilience budgets, the
observability contract, and the config/env matrix.

Every scope item gets a tier — **core** (must ship), **depth** (ship if time holds), or **documented-only** (specified,
not built) — with a recommendation where something looks
over-built for the timebox. Nothing is removed without sign-off.

### CQ-02 — Scaffold and platform package

`go.mod`, directory skeleton, Makefile, `internal/platform`: structured JSON logger (component,
method, line, message, level), config loading, `SecretProvider` reading env with masked logging of
the vendor key, OTel bootstrap and propagation helpers, shared HTTP error rendering. Built first so
every later component is observable from birth rather than retrofitted.

### CQ-03 — Mocked vendor CQAPI

`cmd/cqapi`. Vendor contract per `contract.md`; `api-key` enforcement rejecting missing and wrong
keys identically; configurable random failure and latency injection with a seedable source so tests
are deterministic; commission calculation. Tests: auth rejection, happy path, injected failure.

### CQ-04 — Middleware core and OpenAPI

`cmd/middleware`, `api/openapi.yaml`. Authoritative validation, caller-claim verification, vendor
client attaching `api-key`, error mapping that never leaks vendor auth failures to the caller while
logging the real cause. Tests: validation table, claim rejection, vendor-401 mapping, contract test
asserting handlers match the spec.

### CQ-05 — Middleware resilience

Per-attempt and total timeouts, bounded exponential backoff with jitter, circuit breaker, all to the
budgets fixed in CQ-01. Retries restricted to failures that provably produced no quote. Tests with a
fake vendor: timeout, retry-then-succeed, no-retry-on-4xx, breaker opens and recovers.

### CQ-06 — BFF and AuthProvider

`cmd/bff`. `AuthProvider` interface with the fake in-memory staff session, session cookie handling,
cookie→bearer exchange, proxy to Middleware, UI-friendly error mapping. Holds no business logic and
no vendor credential. Tests: unauthenticated rejection, cookie exchange, error translation.

### CQ-07 — Web front end

`web/`, React + Vite. Loan form with inline validation mirroring `contract.md`, Generate Quote
action, and distinct loading / error / result states. Accessible and responsive. Tests on validation
and state rendering.

### CQ-08 — Edge, compose, README

`deploy/nginx.conf` as single origin with SPA fallback and `/api` → BFF, Dockerfiles,
`docker-compose.yml` wiring all four services. README rewritten with step-by-step run and test
instructions and the AI-usage transparency section the brief requires.

## Per-task workflow

Following `README.md`, each task runs as:

1. Branch `cq-0N-<slug>`.
2. Commit 1 — the planned work for the task (plan captured under `tasks/`), register entry moved to
   in-progress.
3. Commit 2 — the finished implementation and its tests, register entry marked done.
4. PR titled `CQ-0N — <title>`.

## Verification

- **Per task:** `go test ./...` for Go tasks; `npm test` in `web/` for CQ-07. CQ-04 and CQ-05 must
  pass with a fake vendor, no network.
- **End to end after CQ-08:** `docker compose up --build`, open `http://localhost:8080`, sign in as
  the fake staff user, submit a valid quote request and confirm the result renders.
- **Edge cases to demonstrate by hand:** invalid amount and out-of-range term produce inline FE
  errors and are rejected by the Middleware independently; CQAPI started with a forced failure rate
  shows retry then a friendly error, with no vendor key or bearer token in any response or log;
  Middleware started with a wrong `CQAPI_API_KEY` shows a non-security-revealing UI error while the
  real auth failure appears in the Middleware log with the key masked.
- **Isolation check:** `curl` against the Middleware and CQAPI ports must not be reachable through
  the Edge origin.
