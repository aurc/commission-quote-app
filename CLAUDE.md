# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state

This repository is **documentation-only so far** — there is no application code, no `go.mod`, no
`package.json`, and no build/test tooling yet. `design/assumptions.md` is the authoritative design
contract that the implementation must follow; read it before writing any code.

Because nothing is scaffolded, there are no build/lint/test commands to reuse yet. When the first
component is created, add its commands to this file and the run instructions to `README.md` (the
challenge explicitly grades on step-by-step run instructions and the presence of core tests).

## What is being built

A take-home challenge (Bendigo Lending Platform): a staff-facing web app that captures loan details
(`loanAmount`, `loanTermInMonths`, `riskBand`) and returns a commission quote (`quoteId`,
`commissionRate`, `totalCommission`) from a **mocked** external vendor API.

Timebox is 4 hours of implementation. Target is readable code with clean separation of concerns,
a few core tests, and sensible edge-case handling — not a production system. The codebase will later
be extended live in a collaborative coding session, so structure matters more than completeness.

`source/challenge.md` + `source/Code Challenge - Commission Quote App.pdf` hold the original brief.

## Architecture (decided in design/assumptions.md)

Five components behind a single browser-visible origin. The layering is deliberate — do not
short-circuit it (e.g. never let the FE call the Middleware directly, never let the vendor key
reach the BFF or the browser).

| Component      | Stack               | Owns                                                                  |
|----------------|---------------------|-----------------------------------------------------------------------|
| Edge           | nginx               | Single origin, TLS termination, serves FE assets w/ SPA fallback, routes `/api` → BFF |
| FE             | React SPA + Vite    | Form, inline validation, loading/error/result states, presentation formatting |
| BFF            | Tiny Go binary      | Staff session cookie → bearer exchange, UI-friendly errors. No business logic, no vendor credential |
| Middleware     | Go service          | Claim verification, authoritative validation, retries/backoff, circuit breaking, OpenAPI spec, holds the vendor `api-key` |
| CQAPI (mocked) | Standalone Go binary| Vendor contract, `api-key` enforcement, random failure + latency injection |

Abbreviations used throughout the docs: **CQApp** (this app), **CQAPI** (mocked vendor),
**FE**, **BFF**.

### Non-negotiable constraints from the design doc

- **Vendor `api-key` is Middleware-only.** Injected as `CQAPI_API_KEY` through a `SecretProvider`
  interface (env vars in the MVP). Never in FE bundles, never in a BFF response, never past the
  browser boundary. Masked in logs (trailing chars only). CQAPI rejects missing and wrong keys
  identically; the Middleware surfaces a non-security-revealing error to callers while logging the
  real auth failure.
- **Staff identity and vendor auth are separate concerns** — an `AuthProvider` interface (fake
  in-memory staff session in the MVP) authorises the caller into CQApp; the `api-key` authenticates
  CQApp to the vendor. Do not conflate them.
- **Zero trust:** every component verifies the caller's claims. No unprotected resources.
- **Stateless** — no persistence anywhere. Quotes are advisory, non-binding, no lifecycle/expiry/audit.
- **Retries only where no quote was returned.** Quote generation is assumed non-idempotent at the
  vendor, so bounded exponential backoff applies to failures that definitely produced no quote;
  hung requests get a hard timeout.
- **Observability:** structured JSON logs (component, method, line, message, level) and OpenTelemetry
  trace/span propagation across requests. `loanAmount` is business data and may be logged; secrets
  and bearer tokens never are.
- Single currency (AUD), no localisation. View-only staff user, no admin/technical roles.

## Working conventions

The author is running this challenge as an explicit AI-first, transparent workflow:

- Work is broken into discrete tasks tracked in `tasks/register.md`. Every task has a short `code`.
- Each task gets **two commits**: one containing the AI-planned work, one containing the finished
  task.
- PR titles start with the task `code`.
- `README.md` must end up with an AI-usage transparency section (required by the challenge brief).

Expected layout once scaffolded (implied by `.gitignore`): Go binaries build into `/bin/`, the
front end lives in `/web/` and builds to `/web/dist/`.
