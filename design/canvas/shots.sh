#!/bin/sh
# Render the design token sheet from the artboards.
#
# The README's application screenshots are captured from the RUNNING app, not
# from these artboards: once the thing exists, a picture of the design is a
# picture of the wrong thing. Only the token sheet is rendered here, because it
# is a design reference rather than a screen.
#
# Headless Chrome will not size a window below 500px wide: it lays the page out
# at 500 and crops the PNG, which silently produces a phone screenshot of a
# desktop layout. Phones are therefore rendered inside an iframe of exactly the
# phone width and cropped back to it, so the page really sees 390.
set -e

# Regenerate the artboards first. Screenshots rendered from stale artboards show
# a design that no longer exists, and nothing else would catch it.
python3 build.py > /dev/null

CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
WORK=$(mktemp -d)
OUT=../screenshots
mkdir -p "$OUT"

python3 - "$WORK" <<'PY'
import pathlib, re, sys
work = pathlib.Path(sys.argv[1])
for src in sorted(pathlib.Path('.').glob('*.dc.html')):
    s = src.read_text()
    assert '{{' not in s, f'{src} has template holes and cannot be rendered flat'
    style = re.search(r'<helmet>(.*?)</helmet>', s, re.S).group(1)
    body = re.search(r'</helmet>(.*?)</x-dc>', s, re.S).group(1)
    (work / src.name.replace('.dc.html', '.html')).write_text(
        '<!doctype html><html><head><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width, initial-scale=1">'
        f'{style}</head><body>{body}</body></html>')
PY

shot() { # name width height out
  "$CHROME" --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
    --virtual-time-budget=3000 --window-size="$2,$3" \
    --screenshot="$WORK/$4.png" "file://$WORK/$1.html" >/dev/null 2>&1
  sips -Z 1200 "$WORK/$4.png" --out "$OUT/$4.png" >/dev/null
  echo "  $4.png"
}

shot Tokens 1080 780 design-tokens

rm -rf "$WORK"
