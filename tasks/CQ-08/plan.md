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

## The AI usage section

Required by the brief and currently missing. It states plainly: the tool, what it did, what I
directed, and how to check. The commit history is the evidence, so it points at the shape rather than
claiming a level of oversight nobody can verify: a task register, a plan commit before each
implementation commit, one PR per task, and design decisions argued in the PR bodies rather than
asserted.

It should be specific about what review changed, because that is the honest part: the canvas was
wrong four times before it was right.

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

`make check` is green and the health flag was verified against a running service: exit 1 with nothing
listening, 0 while it answers, 1 after it stops.

**The compose stack is written but not verified on this machine.** The Docker daemon here is
20.10.16, four years old, and answers `docker info` while image pulls make no progress at all: a
`golang:1.26` pull produced no output in over thirty minutes and no layers on disk. That is a
connectivity or daemon problem on this machine, not something in the compose file, but it means the
one command path has not been run end to end and should not be claimed as tested. Updating Docker
Desktop is the first thing to try.

Everything the compose file relies on has been verified separately: the services run and talk to each
other natively, the front end works through a proxy in front of the BFF, and the health flag returns
the exit codes compose depends on.
