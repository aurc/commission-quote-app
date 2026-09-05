#!/bin/sh
# Render README screenshots from the artboards, so they cannot show a design
# that no longer exists.
#
# Headless Chrome will not size a window below 500px wide: it lays the page out
# at 500 and crops the PNG, which silently produces a phone screenshot of a
# desktop layout. Phones are therefore rendered inside an iframe of exactly the
# phone width and cropped back to it, so the page really sees 390.
set -e
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
  sips -Z 1400 "$WORK/$4.png" --out "$OUT/$4.png" >/dev/null
  echo "  $4.png"
}

phone() { # name width height out
  python3 - "$WORK" "$1" "$2" "$3" <<'PY'
import pathlib, sys
work, name, w, h = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
pathlib.Path(f'{work}/_frame_{name}.html').write_text(
    '<!doctype html><html><head><meta charset="utf-8"><style>'
    'html,body{margin:0;height:100%;background:#ffffff;}'
    'body{display:flex;justify-content:center;align-items:flex-start;}'
    f'iframe{{width:{w}px;height:{h}px;border:0;display:block;}}</style></head>'
    f'<body><iframe src="./{name}.html" scrolling="no"></iframe></body></html>')
PY
  "$CHROME" --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
    --virtual-time-budget=3000 --window-size="560,$3" \
    --screenshot="$WORK/$4_raw.png" "file://$WORK/_frame_$1.html" >/dev/null 2>&1
  sips -c "$(( $3 * 2 ))" "$(( $2 * 2 ))" "$WORK/$4_raw.png" --out "$WORK/$4.png" >/dev/null
  sips -Z 780 "$WORK/$4.png" --out "$OUT/$4.png" >/dev/null
  echo "  $4.png"
}

shot Main 880 620 quote-result
shot QuoteInvalid 880 940 validation-errors
shot Tokens 1080 780 design-tokens
phone PhoneResult 390 844 phone-result
phone PhoneQuoteInvalid 390 940 phone-validation

rm -rf "$WORK"
