# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Hard rules

- **Never merge a PR.** The repo owner reviews and merges in GitHub. Open PRs, never merge them.
- **Never push to `main`.** Work happens on `cq-0N-<slug>` branches.
- **Pause after the plan.** Commit the task plan, then stop and let the owner review it before
  writing any implementation. Do not run the plan commit and the implementation in one go.
- Prose style in docs and replies: concise, tables over paragraphs, no em dashes.

## Current state

Design and planning are complete. No application code yet: no `go.mod`, no `package.json`, no build
or test tooling. Add commands to this file as each is created.

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
| BFF | Go | 8081 | Session cookie, cookie to bearer exchange, UI friendly errors. No business logic, no vendor credential |
| Middleware | Go | 8082 | Claim verification, authoritative validation, retries, breaker, OpenAPI, holds the `api-key` |
| CQAPI (mock) | Go | 8083 | Vendor contract, `api-key` enforcement, failure and latency injection |

Abbreviations: **CQApp** (this app), **CQAPI** (mocked vendor), **FE**, **BFF**.

## Layout

```
go.mod                    single module
api/openapi.yaml          Middleware published contract, hand written
cmd/{bff,middleware,cqapi}/main.go
internal/
  platform/               log, config, otel, secrets, http helpers
  bff/  middleware/  cqapi/
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

## Workflow

Per task, per `README.md`:

1. Branch `cq-0N-<slug>`.
2. Commit 1: the planned work. Plan at `tasks/CQ-0N/plan.md`, register entry set to `wip`.
3. **Stop here.** The owner reviews the plan before implementation starts.
4. Commit 2: the finished implementation and its tests, register entry set to `done`.
5. Open a PR titled `CQ-0N - <title>`. Do not merge it.

`README.md` must end up with an AI usage transparency section, required by the brief.
