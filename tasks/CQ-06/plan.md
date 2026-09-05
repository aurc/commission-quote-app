# CQ-06 Backend for the Front End

## Context

Nothing a browser can talk to exists yet. The Middleware needs a bearer token, and `make token`
mints one only because there is no BFF; that is the tool this task replaces.

The BFF is where web semantics stop. It owns the staff session, exchanges a cookie for a bearer
claim, and turns the Middleware's API messages into words a person reads. It holds no business logic
and no vendor credential.

Contract in `design/contract.md` sections 5 and 7.

## Deliverables

```
cmd/cqapp-bff/main.go
internal/bff/
  session.go     cookie, in memory store, AuthProvider over the staff fixture
  token.go       cookie to bearer exchange
  quotes.go      proxy to the Middleware
  messages.go    code to user wording
  config.go
```

## Design

### Sign in with a real password flow

The UI signs in with a credential and the API verifies it properly. No fake control, no bypass.

`POST /api/session` takes `staffId` and `password`. `GET /api/session` returns the current staff
member or `401`. `DELETE /api/session` signs out.

**Credentials live in their own file, separate from `config/staff.csv`.**

```
config/staff.csv        id, name, scopes      identity and entitlement
config/credentials.csv  id, passwordHash      credentials, BFF only
```

The Middleware reads the first and must never load the second. It has no business holding password
material it cannot use, and in production these are unambiguously different systems: the IdP holds
credentials and the Middleware never sees them. Putting a hash in the file the Middleware already
reads would put it in the Middleware's memory for no reason.

Two files reintroduce the drift risk CQ-04 removed, so the BFF cross checks at startup: a credential
naming a staff id that does not exist is a startup failure. A staff member without a credential
simply cannot sign in, which is a legitimate state.

**Passwords are bcrypt hashed, including in the fixture.** A committed plaintext password, or a fast
hash like SHA-256, would be wrong in a way that is worth not demonstrating: the fixture is the
example someone copies. `bcrypt.CompareHashAndPassword` is also constant time, so the comparison
does not leak.

**Failure is uniform.** An unknown staff id and a wrong password return the same response, and an
unknown id still costs a bcrypt comparison against a dummy hash, so response timing does not reveal
who exists. Attempts are logged with the staff id and never the password.

Not built, and stated: rate limiting and lockout after repeated failures. They belong here in
production, and `assumptions.md` already places rate limiting in the production column.

### The same fixture, a different column

`AuthProvider` reads `config/staff.csv` through `staffdir`, taking `id` and `name` where the
Middleware takes `id` and `scopes`, and reads the password hash from `config/credentials.csv`. That is the point of CQ-04's fixture work: sign in and
authorisation cannot disagree about who exists.

The BFF does **not** read the `scopes` column to decide anything. It requests `quote:generate` in the
token because that is what the caller is asking to do; the Middleware decides. Reading scopes here to
pre-empt a `403` would rebuild the circularity CQ-04 removed.

### Session cookie

`cq_session`, `HttpOnly`, `SameSite=Lax`, `Path=/`, opaque 256 bit random value, in memory store with
a TTL.

`Secure` is configurable, defaulting on and switched off for the local compose stack. A `Secure`
cookie over `http://localhost` is honoured by some browsers and not others, and a reviewer who cannot
sign in because of their browser choice is a worse outcome than a flag with a documented default.

`SameSite=Lax` is the CSRF control. It withholds the cookie from cross site POSTs entirely, which
covers the only state changing endpoint. A token is not added; the reasoning is recorded so the
absence reads as a decision rather than an oversight.

Sessions are in memory, so a BFF restart signs everyone out and a second replica would not share
them. Now recorded in `assumptions.md` rather than left as an implementation detail, because it is a
property of the deployment, not of the code.

### Not a reverse proxy

`httputil.ReverseProxy` would forward the Middleware's response unchanged, which is exactly the
thing CQ-04 decided against: the Middleware writes API messages and the BFF writes user copy. So a
small typed client instead, which attaches the bearer, forwards the correlation id, and rewrites the
envelope.

Kept from the Middleware's response: `code`, `details`, `correlationId`, and the status. Replaced:
`message`, from the table in `contract.md` section 5. An unrecognised code maps to the `INTERNAL`
wording, so a new Middleware code can never surface to a user as raw API text.

`401` from the Middleware is the one case that is not a passthrough: it means our token was rejected,
which is a BFF or configuration fault, not an expired staff session. Returning it as a `401` would
send the user to sign in again and change nothing. It maps to `502`, on the same reasoning that keeps
a vendor credential failure off a user's screen.

### Transport

The bearer crosses the wire to the Middleware, so the same check the Middleware applies to
`CQAPI_BASE_URL` applies here to `MIDDLEWARE_BASE_URL`: refuse to start on plain HTTP to anything but
the local stack.

### No third OpenAPI file

The Middleware and the vendor publish specs because they have consumers who are not in this
repository. The BFF's only consumer is the front end, which ships with it. The session endpoints go
in `contract.md` instead. Worth revisiting the moment anything else calls it.

## Tests

| Area | Cases |
|---|---|
| Sign in | Correct password succeeds; wrong password, unknown staff id, missing fields and an empty password all fail identically |
| Enumeration | An unknown staff id and a wrong password are indistinguishable in status, body and shape |
| Credentials | A hash is never returned or logged; the password is never logged; the fixture holds no plaintext |
| Startup | A credential naming an unknown staff id fails at startup |
| Session | Cookie flags; sign out invalidates; expiry; a stale cookie is rejected |
| Session cookie | The session value never appears in a response body or a log |
| Token exchange | Claims match `contract.md` section 7; a request without a cookie never reaches the Middleware |
| Wording | Every code maps to user copy; an unknown code maps to the `INTERNAL` wording; no API phrasing survives |
| Passthrough | `code`, `details` and `correlationId` survive; `message` does not |
| Middleware 401 | Becomes `502`, and the user is not told to sign in again |
| Config | Plain HTTP to a remote Middleware is refused at startup |

## Verification

`make check` green. Then the whole stack by hand without `make token`: sign in with the password,
request a quote, sign out, and confirm a quote is refused afterwards. Confirm a wrong password is
refused and is indistinguishable from an unknown user. Confirm an entitled and an unentitled staff
member differ, and that the unentitled one sees words rather than `caller is not entitled to the
required scope`.
