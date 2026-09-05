# Design canvas sources

The artboards behind the design handoff canvas, one `.dc.html` per state, plus `canvas.json` for the
layout. Kept in the repository so the canvas can be amended without starting over.

Rendered to [commission-quote-app-design.pdf](../commission-quote-app-design.pdf) by `pdf.sh`, one
page per artboard. The PDF is the shareable form: the canvas these came from was published as an
Artifact that only opens inside the author's organisation.

`Main.dc.html` is the quote result, the state the application exists to reach. Colours come from
`design/contract.md` section 10; no Bendigo mark appears anywhere, for the reasons in *Branding* in
`design/assumptions.md`.

## Rebuilding

`build.py` generates every artboard. Desk and phone come from one description of each screen, so the
two cannot drift apart in content: only spacing, type scale and the result grid differ.

```sh
python3 build.py          # regenerate the .dc.html artboards
```

Then re-seed and republish the canvas with the `design` skill.

Screenshots in `design/screenshots/` are rendered from these same artboards, so they cannot show a
design that no longer exists.
