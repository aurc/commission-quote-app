# CQ-07.1 Design handoff

## Context

CQ-07 has a contract for what the interface must contain, in `design/contract.md` section 10, and a
palette with measured contrast in the same section. It has no agreed look.

Deciding that in code means deciding it one component at a time, with the cost of changing your mind
rising as the implementation goes on. A canvas puts every state on one surface first, so the look is
reviewed and adjusted before it is committed to.

This task produces no application code. Its output is a published design canvas and, if the review
changes anything, an update to the tokens in `contract.md`.

## Deliverables

- A design canvas published as an Artifact, with one artboard per state
- The canvas URL recorded in this plan and in `tasks/register.md`
- Any token changes agreed during review, written back to `contract.md` section 10

## Artboards

Thirteen, covering every state `contract.md` sections 5 and 10 specify. The error states are the point:
they are the ones a design usually leaves until last and the brief grades explicitly.

| # | Artboard | Shows |
|---|---|---|
| 1 | Tokens | The palette, type scale and spacing, with contrast ratios on the swatches |
| 2 | Sign in | Staff ID and password, the only route in |
| 3 | Sign in refused | The uniform failure, no hint whether the id or the password was wrong |
| 4 | Quote form, idle | The three fields and Generate Quote |
| 5 | Quote form, invalid | Every failed field marked at once, inline, with an error summary |
| 6 | Quote form, submitting | Loading state, control disabled |
| 7 | Quote result | `quoteId`, `commissionRate` as a percentage, `totalCommission` as AUD |
| 8 | Not entitled | The `403`, in words, with the correlation id |
| 9 | Service unavailable | The `502`, `503` and `504` family, and what a user can do about it |
| 10-13 | Phone, 390 wide | Sign in, quote form, invalid and result, the four screens whose layout changes |

## Constraints the canvas has to respect

**No Bendigo mark, name or tagline.** The palette only, and our own wordmark. The reasoning is in
*Branding* in `assumptions.md`. A published artifact has a URL and can be opened by anyone who has
it, so it should not present as a bank's own page.

**Colour is never the only signal.** The brand's action colour is red, which is also the conventional
error colour, so invalid fields need an icon and a message as well as a border.

**Every error state shows the correlation id.** It is the only thing a user can quote that lets an
operator find their request, and it is easy to leave out of a mockup and then out of the code.

**The result panel presents, it does not calculate.** The numbers shown are the vendor's.

## Review, then implement

CQ-07 implements against the approved canvas. Where the two disagree, the canvas wins on look and
`contract.md` wins on behaviour, the same precedence the documents already use.

## Verification

The canvas covers all nine artboards, uses only tokens from `contract.md` section 10, carries no
Bendigo mark, and every error artboard shows a correlation id.

## Canvas

Published at: https://claude.ai/code/artifact/3f6b2b9b-de87-476d-94d7-66fd355e832b

Sources are kept in `design/canvas/` so the canvas can be rebuilt or amended without starting over.
`Main.dc.html` is the quote result, the state the application exists to reach.
