# CQ-02 Scaffold and platform

## Context

No Go code exists. Every later task needs the same cross cutting machinery: structured logging with
a correlation id, config loaded from env, the vendor secret behind an interface, telemetry, and one
error response shape. Building it first means CQ-03 to CQ-06 are observable and consistent from
birth rather than retrofitted, and it keeps `contract.md` sections 5, 8 and 9 in exactly one place.

No HTTP handler logic here. This task ships the module, the tooling, and `internal/platform`.

## Deliverables

```
go.mod                       module github.com/aurc/commission-quote-app
Makefile                     build, test, lint, tidy, run-<svc>
.golangci.yml
internal/platform/
  config/    env loading, typed getters, fail fast on missing required
  secrets/   SecretProvider interface, env implementation, Mask
  logging/   slog JSON handler, context fields, redaction
  telemetry/ OTel tracer provider, W3C propagation, no-op when unexported
  httpx/     error taxonomy rendering, middleware chain, health, graceful serve
cmd/                         .gitkeep only, binaries land in CQ-03 to CQ-06
```

## Design

### config

`config.Load` reads env once into a typed struct per component. Required keys missing means the
process exits non zero with a message naming the key. Defaults come from `contract.md` section 9.

### secrets

```go
type SecretProvider interface { Secret(name string) (string, bool) }
```

Env backed in the MVP. `Mask(s string) string` returns `****<last 4>`, and `****` for anything
shorter than 8 so a short secret is never partly revealed. The interface is the seam the bank secret
manager slots into later.

### logging

`log/slog` with a JSON handler. `AddSource` gives `method` and `line` from `contract.md` section 8.
A wrapping handler pulls `correlationId`, `traceId` and `spanId` off the context so call sites do not
pass them. `component` is bound once at construction.

Redaction is enforced, not merely documented: a `Secret` value type whose `LogValue` returns the
mask, so a secret logged by accident still cannot print in clear.

### telemetry

`telemetry.Init` returns a shutdown func. W3C trace context and baggage propagators always
installed. `OTEL_EXPORTER_OTLP_ENDPOINT` unset means spans are created and propagated but not
exported, per `assumptions.md` 2.3.

### httpx

- `Error` type carrying code, status, user message and optional field details
- Constructors for every class in the `contract.md` section 5 table
- `WriteError` renders the single error envelope and always injects `correlationId`
- Middleware: correlation id (accept inbound `X-Correlation-Id`, else the trace id, else generate),
  tracing, request logging, panic recovery rendering a safe `500`
- `Health` handler and `Serve` with graceful shutdown on SIGTERM

## Tests

| Area | Cases |
|---|---|
| `Mask` | long, exactly 8, short, empty |
| `Secret` | never appears in clear through a JSON log line |
| config | defaults applied, required missing fails, bad int fails |
| logging | `component`, `method`, `line`, context ids present in output |
| httpx | envelope shape matches `contract.md`, status per class, details only on validation |
| middleware | inbound id honoured, generated when absent, echoed on the response |
| recovery | panic renders a safe `500`, no stack in the body |

## Verification

`make lint test` clean, `go test ./... -race` green, no network in tests.
