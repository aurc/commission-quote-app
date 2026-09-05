# Commission Quote App

Full-stack take-home: a web app that captures loan details and returns a commission quote from a
mocked external vendor API.

> Status: design complete, implementation in progress. Run instructions land with CQ-08.

## Documents

| Document | Holds |
|---|---|
| [assumptions.md](design/assumptions.md) | Assumptions, design decisions, scope, delivery tiers |
| [contract.md](design/contract.md) | Schemas, validation rules, commission formula, error taxonomy, resilience budgets, config |
| [register.md](tasks/register.md) | Task register, one PR per task |

`assumptions.md` is the *why*. `contract.md` is the *what*, and is what the code is tested against.

## Architecture

Five components behind a single browser visible origin. The front end never reaches the Middleware,
and the vendor `api-key` never leaves the Middleware.

| Component | Stack | Port | Owns |
|---|---|---|---|
| Edge | nginx | 8080 | Single origin, TLS, serves FE assets with SPA fallback, routes `/api` |
| Web Front End | React, Vite | via Edge | Form, inline validation, loading, error and result states |
| BFF | Go | 8081 | Staff session cookie, cookie to bearer exchange, UI friendly errors |
| Middleware | Go | 8082 | Claim verification, authoritative validation, resilience, holds the `api-key` |
| cqapi-mock | Go | 8083 | Vendor contract, `api-key` enforcement, failure and latency injection |

<img src="design/design.svg">

## Repository structure

```
api/          Middleware OpenAPI contract                    (CQ-04)
cmd/
  cqapp-bff/  BFF entry point                              (CQ-06)
  cqapp-middleware/  Middleware entry point                (CQ-04)
  cqapi-mock/ vendor stand in, named for what it is        (CQ-03)
internal/
  platform/   logging, config, secrets, telemetry            (CQ-02)
  bff/ middleware/ cqapi/                                    (CQ-03 to CQ-06)
web/          React SPA                                      (CQ-07)
deploy/       nginx, Dockerfiles, docker-compose             (CQ-08)
design/       assumptions, contract, diagram
tasks/        task register and per task plans
source/       the original challenge brief
```

Task codes mark what is not built yet.

## Design, Assumptions & Approach

The document [assumptions.md](design/assumptions.md) brings assumptions and design decisions applied to this project.

This work follows an AI first approach, and for transparency I break the work into separate tasks,
with a dedicated commit for the AI planned work and one commit for the finished task.

The task register is at [tasks/register.md](tasks/register.md). Each task has a `code`, and the PRs
that address those tasks quote the `code` at the beginning of the title.

The AI tool utilised is Anthropic Claude Code. I have great familiarity with the tool which I use
both personally and professionally.

A full account of how AI was used lands with CQ-08.
