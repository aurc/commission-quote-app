# CQ-07 Web front end

## Context

Everything the brief asks a user to see is still missing. The API works end to end, but the challenge
is a web application: a form, a Generate Quote button, a display area, and proper loading and error
states.

Contract in `design/contract.md` section 10, which pins what the brief states literally.

## Deliverables

```
web/
  index.html
  package.json                 name cqapp-web
  vite.config.ts               dev proxy for /api to the BFF
  src/
    main.tsx  App.tsx
    api.ts                     session and quote calls
    validation.ts              the section 4 rules, mirrored
    format.ts                  rate and currency presentation
    components/
      SignIn.tsx  QuoteForm.tsx  QuoteResult.tsx  ErrorNotice.tsx
    styles.css
  tests/
```

## Design

### TypeScript, React, Vite, and nothing else

No state library, no component library, no router.

One endpoint and two views do not need TanStack Query or Redux; `fetch` and `useState` are the whole
requirement, and a reviewer should not have to learn a data layer to read a form. Two views do not
need a router either: which one shows is a function of whether there is a session. The Edge still
serves an SPA fallback, so a deep link to any path lands on the app.

Styling is hand written CSS with custom properties. `assumptions.md` 4 says the front end is kept
lean so load times stay low, and pulling in a component library for one form contradicts that. The
brief explicitly allows basic CSS.

### Validation must not drift from the Middleware

The front end mirrors `contract.md` section 4 for immediate feedback and is never trusted; the
Middleware rejects independently. Two implementations of one rule set is exactly the drift this repo
has guarded against everywhere else, and TypeScript cannot import a Go constant.

So the same guard as the Go services: a test parses `api/middleware.openapi.yaml` and asserts the
bounds in `validation.ts` match the published contract. If someone widens the range in one place,
the test fails rather than the two silently disagreeing.

### Errors come from the BFF, already worded

The BFF owns user copy, so the top level `message` is rendered as it arrives. Nothing in the front
end rewrites it, or CQ-06's split collapses back into two places writing the same sentence.

`details[].code` is mapped to per field wording, since a code is the contract and the phrasing is
presentation. An unrecognised field code falls back to a generic message on that field rather than
being dropped, so a new Middleware rule cannot silently produce a form that refuses input without
saying why.

`correlationId` is shown with any failure. It is the only thing a user can quote that lets an
operator find the request.

### Presentation, never recomputation

`commissionRate` renders as a percentage to two decimal places, `0.0180` as `1.80%`, and
`totalCommission` as AUD via `Intl.NumberFormat`. The numbers themselves are the vendor's and are
displayed as received. The front end formats; it does not calculate.

### Responsive, as one stylesheet

The phone artboards in CQ-07.1 are the narrow end of one design, not a second one. Three things
change and nothing else does: the result panel stacks to one column, padding tightens, and the top
bar drops the staff name, which moves into the form so who you are signed in as stays on screen.

Implemented with media queries over the same markup. Two layouts would mean two things to keep in
step, which is the failure mode this repository has avoided everywhere else.

Controls stay 44px or taller, which is why the desk design uses that height too.

### Accessibility from the start, not as polish

Tiered as depth in `assumptions.md`, but most of it is free when done from the beginning and
expensive to retrofit: labels tied to inputs, `aria-invalid` and `aria-describedby` on failed fields,
an error summary that takes focus on submit, `role="alert"` on the async failure, and the submit
control disabled with `aria-busy` while a request is open.

### Sign in

`staffId` and password, posted to `POST /api/session`. On load, `GET /api/session` restores an
existing session, so a refresh does not sign the user out.

The development password is not hinted anywhere in the UI. It is in the README and in
`config/credentials.csv`, and a form that tells you the password is a habit worth not showing.

## Tooling

| Concern | Decision |
|---|---|
| Node | 22 or newer, stated in `package.json` engines and the README |
| Dev server | Vite proxies `/api` to the BFF on 8081, so `npm run dev` needs no Edge |
| Tests | Vitest and React Testing Library |
| Make | `make run-cqapp-web`, `make test-web`, and `make check` runs both suites |

`make test` stays Go only. A Go developer running it should not be told to install Node, and CI can
run the two independently.

## Tests

| Area | Cases |
|---|---|
| Validation | Every rule and boundary in `contract.md` section 4, inside and outside |
| Drift | The published contract's bounds match `validation.ts` |
| States | Idle, loading, result and error each render, and only one at a time |
| Collapse | Submitting collapses the form in place, above the result and never below it; Edit reopens it with the values intact |
| Field errors | `details` map to the right fields; an unknown code still shows something |
| Formatting | `0.0180` renders `1.80%`, `4500` renders as AUD, and neither is recomputed |
| Session | Restored on load; a `401` returns to sign in |
| Accessibility | Inputs have labels, invalid fields are marked, the error summary takes focus |
| Responsive | The result grid stacks below the breakpoint, nothing overflows at 390px |

## Verification

`make check` green. Then the real stack in a browser: sign in as an entitled staff member, generate a
quote, submit invalid numbers and see them marked inline, sign in as `staff-002` and see the refusal
in words rather than a code, and watch the vendor's injected failures surface as a friendly error
rather than a stack trace.
