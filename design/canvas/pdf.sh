#!/bin/sh
# Render every artboard into one PDF, committed to the repository.
#
# The published canvas shares only within the organisation, because it declares
# PNG/PDF export. A reviewer outside it cannot open the link, so the design has
# to travel with the code.
set -e
python3 build.py > /dev/null

CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
WORK=$(mktemp -d)
OUT=../commission-quote-app-design.pdf

python3 - "$WORK" <<'PY'
import pathlib, re, sys

work = pathlib.Path(sys.argv[1])

# Order the pages the way the canvas reads: tokens, then each state on desk,
# then the same states on a phone.
order = ['Tokens', 'SignIn', 'SignInRefused', 'QuoteForm', 'QuoteInvalid',
         'QuoteSubmitting', 'Main', 'NotEntitled', 'Unavailable',
         'PhoneSignIn', 'PhoneSignInRefused', 'PhoneQuoteForm', 'PhoneQuoteInvalid',
         'PhoneQuoteSubmitting', 'PhoneResult', 'PhoneNotEntitled', 'PhoneUnavailable']

titles = {'Main': 'Quote result'}
sections = []
for name in order:
    src = pathlib.Path(f'{name}.dc.html')
    if not src.exists():
        raise SystemExit(f'{src} is missing: run build.py')
    s = src.read_text()
    style = re.search(r'<helmet>(.*?)</helmet>', s, re.S).group(1)
    body = re.search(r'</helmet>(.*?)</x-dc>', s, re.S).group(1)
    phone = name.startswith('Phone')
    label = titles.get(name, re.sub(r'(?<!^)(?=[A-Z])', ' ', name))
    width = 390 if phone else 900
    sections.append((label, width, style if not sections else '', body))

# One page per artboard. The style block is emitted once; the artboards share it.
shared_style = sections[0][2]
pages = []
for label, width, _, body in sections:
    pages.append(
        '<section class="page">'
        f'<h2 class="page__label">{label}</h2>'
        f'<div class="page__frame" style="width: {width}px;">{body}</div>'
        '</section>')

pathlib.Path(f'{work}/all.html').write_text(
    '<!doctype html><html><head><meta charset="utf-8">'
    + shared_style +
    '<style>'
    '  @page { size: A4; margin: 12mm; }'
    '  body { background: #ffffff; }'
    '  .page { break-after: page; }'
    '  .page:last-child { break-after: auto; }'
    '  .page__label { font: 600 13px "IBM Plex Sans", sans-serif; color: #68696b;'
    '                 text-transform: uppercase; letter-spacing: 0.08em; margin: 0 0 10px; }'
    '  .page__frame { border: 1px solid #dee2e6; border-radius: 8px; overflow: hidden; }'
    '</style></head><body>' + ''.join(pages) + '</body></html>')
PY

"$CHROME" --headless --disable-gpu --no-pdf-header-footer --virtual-time-budget=8000 \
  --print-to-pdf="$WORK/out.pdf" "file://$WORK/all.html" >/dev/null 2>&1

cp "$WORK/out.pdf" "$OUT"
rm -rf "$WORK"
echo "wrote $OUT"
