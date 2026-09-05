# Commission Quote App

Full-stack take-home: a web app that captures loan details and returns a commission quote from a
mocked external vendor API.

<p align="center">
  <img src="design/screenshots/quote-result.png" alt="The result: a 1.80% commission rate and a total of $4,500.00, with the form collapsed below to the values it was submitted with" width="760">
</p>

<p align="center">
  <img src="design/screenshots/validation-errors.png" alt="The quote form showing every invalid field at once, each marked inline with an icon and a message" width="380">
  <img src="design/screenshots/phone-result.png" alt="The same result on a 390px phone, entirely above the fold" width="176">
  <img src="design/screenshots/phone-validation.png" alt="Validation errors on a phone" width="176">
</p>

> **These are design mockups, not the running application.** The API works end to end today; the
> front end is CQ-07, and these are replaced with real screenshots when it ships. The full canvas,
> thirteen artboards covering every state on desk and phone, is [the design handoff](tasks/CQ-07.1/plan.md).

> Status: implementation in progress. The API is complete; the front end is next. The single
> `docker compose up` path lands with CQ-08; native development works today.

| Built                                                    | Not yet                                         |
|----------------------------------------------------------|-------------------------------------------------|
| Vendor mock, Middleware, resilience, BFF, design handoff | Web front end (CQ-07), Edge and compose (CQ-08) |

## Documents

| Document                                               | Holds                                                                                           |
|--------------------------------------------------------|-------------------------------------------------------------------------------------------------|
| [assumptions.md](design/assumptions.md)                | Assumptions, design decisions, scope, delivery tiers                                            |
| [contract.md](design/contract.md)                      | Schemas, validation rules, commission formula, error taxonomy, resilience budgets, auth, config |
| [register.md](tasks/register.md)                       | Task register, one PR per task                                                                  |
| [cqapi.openapi.yaml](api/cqapi.openapi.yaml)           | The vendor's published contract                                                                 |
| [middleware.openapi.yaml](api/middleware.openapi.yaml) | Our published contract                                                                          |
| [design/canvas/](design/canvas/)                       | Design canvas sources, one artboard per state                                                   |

`assumptions.md` is the *why*. `contract.md` is the *what*, and is what the code is tested against.

## Architecture

Five components behind a single browser visible origin. The front end never reaches the Middleware,
and the vendor `api-key` never leaves the Middleware.

| Component     | Stack       | Port     | Owns                                                                                       |
|---------------|-------------|----------|--------------------------------------------------------------------------------------------|
| Edge          | nginx       | 8080     | Single origin, serves FE assets with SPA fallback, routes `/api`                           |
| Web Front End | React, Vite | via Edge | Form, inline validation, loading, error and result states                                  |
| BFF           | Go          | 8081     | Staff session cookie, cookie to bearer exchange, user facing error wording                 |
| Middleware    | Go          | 8082     | Claim verification, entitlement, authoritative validation, resilience, holds the `api-key` |
| cqapi-mock    | Go          | 8083     | Stand in for the vendor: contract, `api-key` enforcement, failure and latency injection    |

<img src="design/design.svg">

## Repository structure

```
api/          published OpenAPI contracts
cmd/
  cqapp-middleware/  middleware entry point                (CQ-04)
  cqapp-bff/         BFF entry point                       (CQ-06)
  cqapi-mock/        vendor stand in, named for what it is (CQ-03)
  devtoken/          development only, see below
  devstaff/          development only, adds a staff member
internal/
  platform/         shared: logging, config, secrets, telemetry, money, http, tokens
  cqappbff/         -> cmd/cqapp-bff
  cqappmiddleware/  -> cmd/cqapp-middleware
  cqapimock/        -> cmd/cqapi-mock
web/          React SPA                                    (CQ-07)
deploy/       nginx, Dockerfiles, docker compose            (CQ-08)
config/       staff.csv and credentials.csv, the identity fixtures
design/       assumptions, contract, diagram
tasks/        task register and per task plans
source/       the original challenge brief
```

Components we own carry the `cqapp-` prefix, in `cmd/` and in `internal/` alike, so a package name
says which service it belongs to. `cqapi-mock` does not carry it, because it is not ours: it stands in
for the vendor and is deleted when the real one arrives.

Directory names use a hyphen where they become an artifact, a binary, an image or a compose service.
Go package names cannot, so `cmd/cqapp-bff` pairs with `internal/cqappbff`.

## Design, Assumptions & Approach

The document [assumptions.md](design/assumptions.md) brings assumptions and design decisions applied to this project.

This work follows an AI first approach, and for transparency I break the work into separate tasks,
with a dedicated commit for the AI planned work and one commit for the finished task.

The task register is at [tasks/register.md](tasks/register.md). Each task has a `code`, and the PRs
that address those tasks quote the `code` at the beginning of the title.

The AI tool utilised is Anthropic Claude Code. I have great familiarity with the tool which I use
both personally and professionally.

A full account of how AI was used lands with CQ-08.

## Getting started

Requires Go 1.26. No other tooling; `golangci-lint` is used if installed and skipped if not.

```sh
make env     # writes .env from .env.example, development values only
```

`.env` is gitignored. Nothing in `.env.example` is a secret; in a deployed environment every value
comes from the secret manager through the same `SecretProvider` interface.

### Running

Two terminals, or background them:

```sh
make run-cqapi-mock          # vendor stand in, port 8083
make run-cqapp-middleware    # middleware,      port 8082
make run-cqapp-bff           # bff,             port 8081
```

### Making a request

Sign in, then ask for a quote. The development password for every fixture row is `demo-password`,
stated in [config/credentials.csv](config/credentials.csv).

```sh
curl -s -c jar localhost:8081/api/session \
  -d '{"staffId":"staff-001","password":"demo-password"}'

curl -s -b jar localhost:8081/api/v1/quotes \
  -d '{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}'
```

```json
{
  "quoteId": "7c4677e6-...",
  "commissionRate": 0.0180,
  "totalCommission": 4500.00
}
```

Staff are listed in [config/staff.csv](config/staff.csv), which stands in for the identity provider,
and their credentials in [config/credentials.csv](config/credentials.csv), which only the BFF reads.
`make dev-staff ARGS='-id staff-004 -name "Jane Doe"'` adds a member, prompting for a password and
hashing it.

Things worth trying:

```sh
# staff-002 signs in fine, but holds no scopes            -> 403
curl -s -c jar2 localhost:8081/api/session -d '{"staffId":"staff-002","password":"demo-password"}'
curl -s -b jar2 localhost:8081/api/v1/quotes -d '{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}'

# a wrong password and an unknown staff id are the same answer
curl -s localhost:8081/api/session -d '{"staffId":"staff-001","password":"wrong"}'
curl -s localhost:8081/api/session -d '{"staffId":"nobody","password":"demo-password"}'

# every invalid field at once                             -> 400
curl -s -b jar localhost:8081/api/v1/quotes -d '{"loanAmount":1,"loanTermInMonths":9999,"riskBand":"Z"}'

# signing out invalidates the session server side         -> 401 afterwards
curl -s -b jar -X DELETE localhost:8081/api/session
```

The vendor mock fails 15% of requests and stalls another 10% past the Middleware's budget, because
the challenge asks it to misbehave. The Middleware absorbs most of that; set `CQAPI_FAILURE_RATE=0`
in `.env` to stop it entirely.

`make dev-token` still mints a bearer for calling the Middleware directly on port 8082, without the BFF.

## Testing

```sh
make test     # go test -race ./...
make cover    # coverage per package
make lint     # go vet, then golangci-lint if installed
make check    # fmt, vet and test. Run this before opening a PR
```

Tests use `httptest` fakes and never touch the network, so they do not need either service running.
`.env` is deliberately not exported into `make test`: a test that passes or fails depending on a
developer's environment is worse than no test.

## Make targets

```sh
make help
```

Every target is named after the thing it acts on, so `cmd/cqapp-bff` is `make run-cqapp-bff`.
Development only tools carry a `dev-` prefix.

| Target                                                    | Does                                                         |
|-----------------------------------------------------------|--------------------------------------------------------------|
| `env`                                                     | Create `.env` from `.env.example`                            |
| `run-cqapi-mock`, `run-cqapp-middleware`, `run-cqapp-bff` | Run one service natively                                     |
| `dev-staff`                                               | Add a staff member, prompting for a password                 |
| `dev-token`                                               | Print a bearer token, to call the middleware without the BFF |
| `build`                                                   | Build every service into `bin/`                              |
| `test`, `cover`, `vet`, `lint`, `check`                   | See above                                                    |
| `fmt`, `tidy`, `clean`                                    | Housekeeping                                                 |
