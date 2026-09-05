# CQ-08 Edge, compose and submission

## Context

Four components run, and running them means four terminals, a `.env`, and knowing which port is
which. The brief asks for clear step by step instructions to start the application and run the
tests, and grades them. It also requires a section saying how AI was used, which does not exist yet.

This task makes the whole thing one command, and finishes the submission.

## Deliverables

```
deploy/
  Dockerfile.service   one file, any Go service
  Dockerfile.edge      build the SPA, serve it from nginx
  nginx.conf           single origin, SPA fallback, /api to the BFF
  compose.yaml
design/canvas/pdf.sh   the canvas as a committed PDF
README.md              run instructions, and the AI usage section the brief requires
```

## Design

### One Dockerfile for three services

All three Go binaries share a module, so `deploy/Dockerfile.go` takes `ARG SERVICE` and builds any
of them. Three near identical Dockerfiles would drift, and the first one to drift would be the one
nobody rebuilt.

Base image `gcr.io/distroless/static:nonroot`: a static Go binary needs nothing else, it carries CA
certificates for the day the vendor is real and HTTPS, and it has no shell, so a container that is
somehow reached cannot be explored from inside. Non root by default.

### Health checks without a shell

Distroless has no shell and no curl, so the usual compose `healthcheck` command cannot run. Each
binary therefore accepts `-health`, which requests its own `/healthz` and exits 0 or 1. About twenty
lines, no dependency, and it makes `depends_on: service_healthy` actually work rather than the
services racing at startup.

### Only the Edge is published

`ports:` on the edge alone. The Middleware, the BFF and the vendor mock are reachable on the compose
network and from nowhere else, which is the isolation `assumptions.md` claims and the verification
step checks.

### It has to work from a clean clone

`.env` is gitignored, so a fresh clone has none, and a compose file that requires one fails on the
first thing a reviewer types. Compose substitutes documented development defaults,
`${CQAPI_API_KEY:-...}`, so `docker compose up` works immediately, and a `.env` overrides them when
present. The defaults are the same values as `.env.example`, which is already committed and labelled
as not secret.

### Fixtures

`config/staff.csv` and `config/credentials.csv` are copied into the images, so the stack is
self contained, and also mounted read only in compose, so editing a fixture and restarting works
without a rebuild.

### nginx

Serves the built SPA with `try_files $uri /index.html`, proxies `/api` to the BFF, and sets the
security headers a bank's own scanner will look for: `Content-Security-Policy`,
`X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`. Correlation ids pass through
untouched.

TLS terminates here in a deployed environment. Locally it is HTTP, for the reason already recorded in
`assumptions.md`.

### The canvas as a PDF

The canvas shares only within the organisation because it declares export, so a reviewer outside it
cannot open the link. `pdf.sh` renders every artboard into one PDF committed to the repository, so
the design travels with the code and needs no link at all.

## AI usage

The brief asks for a brief section on how AI was used. It belongs in the existing approach section
rather than a separate one: that section already names the tool and describes the workflow, and the
register, the commit history and the PR bodies are the actual account. A longer write up would be a
claim about the work sitting next to the evidence for it.

## Tests

Compose is not unit testable, so this task is verified by running it.

| Check | Expected |
|---|---|
| Clean clone, no `.env` | `docker compose up --build` serves the app on 8080 |
| Sign in and quote through the Edge | A quote, end to end |
| Middleware and vendor ports | Not reachable from the host |
| SPA fallback | A deep link returns the app, not a 404 |
| Security headers | Present on the Edge response |
| A fixture edit | Visible after a restart, without a rebuild |
| `make check` | Still green |

## Verification

The reviewer path, exactly as written in the README, from a clean checkout in a temporary directory.

## Verification status

Run end to end against the built stack, on Docker 29.7.2.

| Check | Result |
|---|---|
| `docker compose up --build` from a clean state | All four containers healthy, in dependency order |
| The SPA on the single origin | `200` |
| A deep link | `200`, and the app rather than a 404 |
| Sign in and a quote through the Edge | A quote with a real vendor id |
| Invalid input | `400`, every failed field at once |
| 8081, 8082, 8083 from the host | Connection refused; only the Edge publishes |
| Security headers | CSP, nosniff, no-referrer, DENY, `server_tokens off` |
| A fixture edit, backends restarted only | The new staff member signs in and gets a quote |
| Image sizes | 17 MB per Go service, 50 MB for the Edge |

### One defect this found

The first run returned `502` from the Edge after restarting the BFF. The BFF was healthy and
reachable on the network; nginx was still dialling its old address.

nginx resolves an upstream name once at startup and caches it forever, so any backend that restarts
comes back on a new address and every request fails until nginx restarts too. In a deployed
environment that turns a routine rolling restart into an outage.

Fixed by naming the upstream in a variable with Docker's embedded resolver, which forces a lookup per
request. Re-tested: restarting both backends no longer disturbs the Edge.

This is the reason the task is verified by running it rather than by reading it. Nothing in the
compose file or the nginx config looked wrong.

## Postman collection

Added during CQ-08 at review's suggestion: `postman/`, one folder per component so each API can be
exercised on its own.

Every request carries assertions, so the collection is a smoke test as well as a set of examples, and
`make smoke` runs it headlessly with newman. 19 requests, 29 assertions.

The Middleware folder mints its own HS256 bearer in a pre-request script, so that service can be
driven without the BFF and without `make dev-token`.

Reaching those services needed solving properly. The first instruction was to run them natively
alongside compose, which quietly created a second stack: the Postman requests hit a native Middleware
talking to a native vendor, not the compose ones, so the collection was testing something other than
what was running. `deploy/compose.debug.yaml` publishes them on loopback instead, as an explicit opt
out of an isolation worth keeping by default.

Writing it found a bug in the collection rather than the application. The pre-request script read
`pm.environment.get('scope') || 'quote:generate'`, and an empty scope is falsy, so the case testing a
token that does not request the scope still requested it and returned 200. The assertion caught it.
An empty value that is also a meaningful value cannot go through a `||` default.

## Continuous integration

Added at review's suggestion, and a real gap: nothing was checking the build or coverage other than
someone remembering to run `make check`.

Three jobs, one per way the project can break, each mirroring a make target so a failure reproduces
locally. Coverage is gated on `internal/` at 80%, currently 86.8%, and on the front end at 80%
statements, currently 88.3%. The gate deliberately excludes `cmd/`, which is wiring covered by the
stack job rather than by unit tests; including it would only teach people to ignore the number.

Adding the gate immediately found something. `internal/platform/authtoken` had **no tests at all**,
despite being the token signing and verification package: the most security sensitive code here. It
was exercised indirectly through the Middleware, which is why nothing looked wrong. It now has its
own tests for algorithm pinning, a wrong key, expiry, a missing expiry, wrong issuer and audience, a
blank subject, clock skew leeway, and unique token ids.
