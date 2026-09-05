# Tasks

Work tracker. Each task is one PR, titled `<code> - <title>`.

Statuses: `todo`, `in progress`, `done`.

| Code  | Title                       | Status | Deliverable                                            |
|-------|-----------------------------|--------|--------------------------------------------------------|
| CQ-01 | Design completeness pass    | done   | `design/contract.md`, repaired `design/assumptions.md` |
| CQ-02 | Scaffold and platform       | done   | `go.mod`, `Makefile`, `internal/platform`              |
| CQ-03 | Mocked vendor CQAPI         | done   | `cmd/cqapi-mock`, `api/cqapi.openapi.yaml`                  |
| CQ-04 | Middleware core and OpenAPI | done   | `cmd/cqapp-middleware`, `api/middleware.openapi.yaml`        |
| CQ-05 | Middleware resilience       | todo   | timeouts, retries, circuit breaker                     |
| CQ-06 | BFF and AuthProvider        | todo   | `cmd/cqapp-bff`                                              |
| CQ-07 | Web front end               | todo   | `web/`                                                 |
| CQ-08 | Edge, compose, README       | todo   | `deploy/`, run instructions, AI transparency           |

## Task detail

### CQ-01 Design completeness pass

Docs only. Close the gaps in `design/assumptions.md` that block implementation, add
`design/contract.md` with the testable detail, tier every scope item against the 4 hour timebox.

### CQ-02 Scaffold and platform

Single Go module, `cmd/` per binary. `internal/platform`: structured JSON logger, config,
`SecretProvider` with key masking, OpenTelemetry bootstrap and propagation, shared HTTP error
rendering. Built first so later components are observable from birth.

### CQ-03 Mocked vendor CQAPI

Vendor contract per `contract.md`, published as `api/cqapi.openapi.yaml`. `api-key` enforcement,
seedable failure and latency injection, commission calculation. The vendor owns the formula; nothing
downstream recomputes it.

### CQ-04 Middleware core and OpenAPI

Authoritative validation, caller claim verification, vendor client, error mapping that never leaks
vendor auth failures to the caller. Published as `api/middleware.openapi.yaml`, which starts close to
the vendor spec and is expected to diverge.

### CQ-05 Middleware resilience

Per attempt and total timeouts, bounded backoff with jitter, circuit breaker. Retries only on
failures that provably produced no quote.

### CQ-06 BFF and AuthProvider

`AuthProvider` interface with in memory staff session, session cookie, cookie to bearer exchange,
proxy to Middleware. Owns user facing error wording, mapping the Middleware's `code` to what the
browser shows, per `contract.md` section 5. No business logic, no vendor credential.

### CQ-07 Web front end

React and Vite. Loan form with inline validation mirroring `contract.md`, distinct loading, error
and result states. Accessible and responsive.

### CQ-08 Edge, compose, README

nginx single origin with SPA fallback and `/api` routing, Dockerfiles, `docker-compose.yml`,
README run and test instructions, AI usage transparency section.

## Workflow

Per task: branch `cq-0N-<slug>`, one commit for the planned work, one commit for the finished task,
then a PR. Plans live in `tasks/CQ-0N/plan.md`.
