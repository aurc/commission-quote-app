#!/usr/bin/env python3
"""Generate the design canvas artboards.

Desktop and phone artboards are generated from one description of each screen, so
the two cannot drift apart in content. Only spacing, type scale and the result
grid differ; the words, fields and states are shared by construction.

    python3 build.py

Then re-seed and republish the canvas: see README.md.
"""

HEAD = '''<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500&display=swap">
  <style>
    body { margin: 0; background: #f6f2f5; color: #212529;
           font-family: 'IBM Plex Sans', 'Segoe UI', system-ui, sans-serif; }
    a { color: #870e40; } a:hover { color: #58003a; }
    * { box-sizing: border-box; }
  </style>
</helmet>
'''
FOOT = '</x-dc>\n</body>\n</html>\n'

MARK = '''<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="1.75" stroke-linecap="round" aria-hidden="true">
        <circle cx="7.5" cy="7.5" r="3.25"></circle>
        <circle cx="16.5" cy="16.5" r="3.25"></circle>
        <line x1="18.5" y1="5.5" x2="5.5" y2="18.5"></line>
      </svg>'''


class Scale:
    """Everything that differs between a desk and a phone."""

    def __init__(self, mobile):
        self.mobile = mobile
        self.bar_pad = '0 16px' if mobile else '0 32px'
        self.page_pad = '20px 16px 32px' if mobile else '40px 32px 48px'
        self.card_pad = '20px' if mobile else '28px'
        self.h1 = 20 if mobile else 24
        self.hero = 26 if mobile else 32
        self.result_cols = 1 if mobile else 2
        # A phone shows who is signed in on the form, not in the bar: the bar has
        # room for the wordmark and one control, and the control is the one you
        # cannot get to any other way.
        self.bar_name = not mobile


def alert_icon(colour="#b20838", size=16):
    return (f'<svg width="{size}" height="{size}" viewBox="0 0 16 16" fill="none" stroke="{colour}" '
            'stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" '
            'style="flex: none; margin-top: 1px;" aria-hidden="true">'
            '<path d="M8 2.5 14.5 13.5H1.5Z"></path><line x1="8" y1="6.4" x2="8" y2="9.4"></line>'
            f'<circle cx="8" cy="11.6" r="0.6" fill="{colour}" stroke="none"></circle></svg>')


def topbar(s, staff=None):
    right = ''
    if staff:
        name = (f'<span style="font-size: 14px; color: rgba(255, 255, 255, 0.85);">{staff}</span>'
                if s.bar_name else '')
        right = (f'<div style="display: flex; align-items: center; gap: 16px;">{name}'
                 '<button style="height: 36px; padding: 0 14px; border: 1px solid rgba(255, 255, 255, 0.4); '
                 "border-radius: 6px; background: transparent; color: #ffffff; font: 500 14px 'IBM Plex Sans', sans-serif;\">"
                 'Sign out</button></div>')
    return f'''  <header style="height: 64px; padding: {s.bar_pad}; background: #870e40; display: flex; align-items: center; justify-content: space-between;">
      <div style="display: flex; align-items: center; gap: 12px;">
        {MARK}
        <span style="font-size: 17px; font-weight: 600; color: #ffffff;">Commission Quote</span>
      </div>
      {right}
  </header>
'''


def page(s, body, staff=None, width=560):
    return (HEAD + '\n<div style="min-height: 100%; display: flex; flex-direction: column;">\n'
            + topbar(s, staff)
            + f'''  <main style="flex-grow: 1; padding: {s.page_pad}; display: flex; justify-content: center;">
    <div style="width: 100%; max-width: {width}px; display: flex; flex-direction: column; gap: 20px;">
{body}
    </div>
  </main>
</div>
''' + FOOT)


LABEL = "display: block; font: 500 14px 'IBM Plex Sans', sans-serif; color: #212529; margin-bottom: 6px;"
INPUT = ("width: 100%; height: 44px; padding: 0 12px; border: 1px solid #dee2e6; border-radius: 6px; "
         "background: #ffffff; color: #212529; font: 400 16px 'IBM Plex Sans', sans-serif;")
INPUT_BAD = INPUT.replace('border: 1px solid #dee2e6', 'border: 2px solid #b20838')
HINT = 'font-size: 13px; line-height: 1.45; color: #68696b; margin-top: 6px;'
ERRTXT = ("font-size: 13px; line-height: 1.45; color: #b20838; margin-top: 6px; "
          "display: flex; gap: 6px; align-items: flex-start;")
BTN = ("width: 100%; height: 48px; border: none; border-radius: 6px; background: #870e40; "
       "color: #ffffff; font: 600 16px 'IBM Plex Sans', sans-serif;")
MONO = "font: 400 14px 'IBM Plex Mono', monospace;"


def card(s, extra=''):
    return f'background: #ffffff; border: 1px solid #dee2e6; border-radius: 8px; padding: {s.card_pad};{extra}'


def corr(cid, bordered=True):
    top = 'margin-top: 14px; padding-top: 14px; border-top: 1px solid #dee2e6;' if bordered else 'margin-top: 10px;'
    return (f'<div style="display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; {top}">'
            '<span style="font-size: 13px; color: #68696b;">Reference</span>'
            f'<span style="{MONO} color: #212529;">{cid}</span>'
            '<span style="font-size: 13px; color: #68696b;">quote this if you contact support</span></div>')


def field(label, value, hint=None, error=None, prefix=None):
    inner = f'<input value="{value}" style="{INPUT_BAD if error else INPUT}{" padding-left: 30px;" if prefix else ""}">'
    if prefix:
        inner = ('<div style="position: relative;">'
                 '<span style="position: absolute; left: 12px; top: 11px; font-size: 16px; color: #68696b;">'
                 f'{prefix}</span>{inner}</div>')
    out = [f'        <div>\n          <label style="{LABEL}">{label}</label>\n          {inner}']
    if error:
        out.append(f'          <div style="{ERRTXT}">{alert_icon()}<span>{error}</span></div>')
    elif hint:
        out.append(f'          <div style="{HINT}">{hint}</div>')
    out.append('        </div>')
    return '\n'.join(out)


def band(value="B — Standard risk", error=None):
    tail = (f'          <div style="{ERRTXT}">{alert_icon()}<span>{error}</span></div>' if error
            else f'          <div style="{HINT}">A low, B standard, C elevated.</div>')
    return f'''        <div>
          <label style="{LABEL}">Risk band</label>
          <div style="position: relative;">
            <select style="{INPUT_BAD if error else INPUT} appearance: none; padding-right: 36px;">
              <option>{value}</option>
            </select>
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#68696b" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" style="position: absolute; right: 14px; top: 15px; pointer-events: none;" aria-hidden="true"><path d="M4 6.5 8 10.5l4-4"></path></svg>
          </div>
{tail}
        </div>'''


def signed_in_as(s, staff):
    """On a phone the bar has no room for a name, so the form carries it."""
    if s.bar_name:
        return ''
    return ('        <div style="font-size: 13px; color: #68696b; margin: -8px 0 4px;">Signed in as '
            f'<span style="color: #212529; font-weight: 500;">{staff}</span></div>\n')


def form(s, staff, amount="250000.00", term="240", errors=None, submitting=False, summary=None):
    errors = errors or {}
    parts = [f'''      <div style="{card(s)}">
        <h1 style="margin: 0 0 4px; font-size: {s.h1}px; font-weight: 600; line-height: 1.25;">Generate a commission quote</h1>
        <p style="margin: 0 0 20px; font-size: 15px; line-height: 1.5; color: #68696b;">Enter the loan details. The quote is advisory and is not binding.</p>
        <div style="display: flex; flex-direction: column; gap: 18px;">''']
    header = signed_in_as(s, staff)
    if header:
        parts.append(header.rstrip('\n'))
    if summary:
        parts.append(summary)
    parts.append(field("Loan amount", amount, hint="Between 1,000.00 and 5,000,000.00, in dollars and cents.",
                       error=errors.get('amount'), prefix="$"))
    parts.append(field("Loan term in months", term, hint="Between 6 and 360 months.", error=errors.get('term')))
    parts.append(band(errors.get('band_value', "B — Standard risk"), error=errors.get('band')))
    if submitting:
        parts.append(f'''          <button disabled style="{BTN} opacity: 0.55; display: flex; align-items: center; justify-content: center; gap: 10px;">
            <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
              <circle cx="9" cy="9" r="7" stroke="rgba(255, 255, 255, 0.35)" stroke-width="2"></circle>
              <path d="M9 2a7 7 0 0 1 7 7" stroke="#ffffff" stroke-width="2" stroke-linecap="round"></path>
            </svg>
            <span>Generating quote</span>
          </button>
          <div style="font-size: 13px; color: #68696b; text-align: center;">Contacting the quote service. This usually takes a moment.</div>''')
    else:
        parts.append(f'          <button style="{BTN}">Generate Quote</button>')
    parts.append('        </div>\n      </div>')
    return '\n'.join(parts)


SUMMARY = f'''          <div style="display: flex; gap: 10px; padding: 14px 16px; border: 1px solid #b20838; border-radius: 6px; background: #ffffff;">
            {alert_icon(size=18)}
            <div>
              <div style="font-size: 15px; font-weight: 500; color: #b20838; line-height: 1.4;">Check the highlighted fields.</div>
              <ul style="margin: 8px 0 0; padding-left: 18px; font-size: 14px; line-height: 1.6; color: #212529;">
                <li><a href="#" style="color: #b20838;">Loan amount is outside the accepted range</a></li>
                <li><a href="#" style="color: #b20838;">Loan term is outside the accepted range</a></li>
                <li><a href="#" style="color: #b20838;">Risk band is not one of A, B or C</a></li>
              </ul>
            </div>
          </div>'''

INVALID_ERRORS = {'amount': "Enter an amount between $1,000.00 and $5,000,000.00.",
                  'term': "Enter a term between 6 and 360 months.",
                  'band': "Select a risk band.",
                  'band_value': "Choose a band"}


def result_panel(s):
    """The answer, at the top of the page.

    White, because it is now the primary surface: the page ground is the tint,
    so a tinted panel would barely separate from it.
    """
    return f'''      <div style="background: #ffffff; border: 1px solid #dee2e6; border-radius: 8px; padding: {s.card_pad};">
        <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 18px;">
          <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="#870e40" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3.5 9.5 7 13l7.5-8"></path></svg>
          <h2 style="margin: 0; font-size: 18px; font-weight: 600;">Quote generated</h2>
        </div>
        <div style="display: grid; grid-template-columns: repeat({s.result_cols}, minmax(0, 1fr)); gap: 18px;">
          <div>
            <div style="font-size: 13px; color: #68696b; margin-bottom: 4px;">Commission rate</div>
            <div style="font-size: {s.hero}px; font-weight: 600; color: #870e40; line-height: 1.1;">1.80%</div>
          </div>
          <div>
            <div style="font-size: 13px; color: #68696b; margin-bottom: 4px;">Total commission</div>
            <div style="font-size: {s.hero}px; font-weight: 600; color: #870e40; line-height: 1.1;">$4,500.00</div>
          </div>
        </div>
        <div style="margin-top: 18px; padding-top: 16px; border-top: 1px solid #dee2e6;">
          <div style="font-size: 13px; color: #68696b; margin-bottom: 4px;">Quote ID</div>
          <div style="{MONO} color: #212529; word-break: break-all;">7c4677e6-b95b-4ee8-bcf5-c17bbda9d63a</div>
        </div>
        <p style="margin: 16px 0 0; font-size: 13px; line-height: 1.5; color: #68696b;">
          Advisory only and not binding. Nothing is stored, so generate a new quote rather than returning to this one.
        </p>
      </div>'''


def collapsed_form(s, staff):
    """What was asked, once the answer is on screen.

    The form's job is done when it is submitted, and a full form above the
    result pushes the answer off a phone screen. It collapses to the values it
    was submitted with, which the result needs anyway: a quote without its
    inputs cannot be checked, and after an edit you could not tell which numbers
    produced it.
    """
    name = (f'''        <div style="font-size: 13px; color: #68696b; margin-bottom: 10px;">Signed in as <span style="color: #212529; font-weight: 500;">{staff}</span></div>'''
            if not s.bar_name else '')
    return f'''      <div style="background: #f6f2f5; border: 1px solid #dee2e6; border-radius: 8px; padding: {s.card_pad};">
{name}
        <div style="display: flex; flex-wrap: wrap; gap: 12px; align-items: center; justify-content: space-between;">
          <div>
            <div style="font-size: 13px; color: #68696b; margin-bottom: 2px;">Quote for</div>
            <div style="font-size: 15px; font-weight: 500; color: #212529;">$250,000.00 &middot; 240 months &middot; Band B</div>
          </div>
          <button style="height: 44px; padding: 0 18px; border: 1px solid #870e40; border-radius: 6px; background: #ffffff; color: #870e40; font: 600 15px 'IBM Plex Sans', sans-serif;">Edit</button>
        </div>
      </div>'''


def signin(s, refused=False):
    banner = ''
    if refused:
        banner = f'''        <div style="display: flex; gap: 10px; padding: 14px 16px; margin-bottom: 20px; border: 1px solid #b20838; border-radius: 6px; background: #ffffff;">
          {alert_icon(size=18)}
          <div>
            <div style="font-size: 15px; font-weight: 500; color: #b20838; line-height: 1.4;">That staff ID or password is not correct.</div>
            <div style="font-size: 13px; color: #68696b; margin-top: 4px; line-height: 1.45;">Check both and try again.</div>
            {corr("05f2077a1e6e8e3a", bordered=False)}
          </div>
        </div>'''
    return f'''      <div style="{card(s)}">
        <h1 style="margin: 0 0 4px; font-size: {s.h1}px; font-weight: 600; line-height: 1.25;">Sign in</h1>
        <p style="margin: 0 0 20px; font-size: 15px; line-height: 1.5; color: #68696b;">Commission quotes for lending staff.</p>
{banner}
        <div style="display: flex; flex-direction: column; gap: 18px;">
{field("Staff ID", "staff-001")}
{field("Password", "•••••••" if refused else "•••••••••••••")}
          <button style="{BTN}">Sign in</button>
        </div>
      </div>'''


def not_entitled(s):
    return f'''      <div style="{card(s)}">
        <div style="display: flex; gap: 12px;">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="#870e40" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" style="flex: none; margin-top: 2px;" aria-hidden="true">
            <rect x="4" y="9" width="12" height="8" rx="1.5"></rect><path d="M7 9V6.5a3 3 0 0 1 6 0V9"></path>
          </svg>
          <div>
            <h1 style="margin: 0 0 6px; font-size: {s.h1}px; font-weight: 600; line-height: 1.3;">You do not have access to generate quotes.</h1>
            <p style="margin: 0; font-size: 15px; line-height: 1.5; color: #68696b;">
              You are signed in, so signing in again will not help. Ask your manager to have quote access added to your profile.
            </p>
            {corr("2071b1bf66a4678d")}
          </div>
        </div>
      </div>'''


def unavailable(s):
    return f'''      <div style="{card(s)}">
        <div style="display: flex; gap: 12px;">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="#870e40" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" style="flex: none; margin-top: 2px;" aria-hidden="true">
            <circle cx="10" cy="10" r="7.5"></circle><path d="M10 6v4.5l3 1.8"></path>
          </svg>
          <div style="flex-grow: 1;">
            <h1 style="margin: 0 0 6px; font-size: {s.h1}px; font-weight: 600; line-height: 1.3;">Quotes are unavailable right now. Try again shortly.</h1>
            <p style="margin: 0 0 18px; font-size: 15px; line-height: 1.5; color: #68696b;">
              The quote service did not respond. No quote was created, so it is safe to try again.
            </p>
            <button style="height: 44px; padding: 0 20px; border: 1px solid #870e40; border-radius: 6px; background: #ffffff; color: #870e40; font: 600 15px 'IBM Plex Sans', sans-serif;">Try again</button>
            {corr("71ce95ac0b87a1a8")}
          </div>
        </div>
      </div>'''


STAFF = "Aurelio Calegari"

def build(mobile):
    s = Scale(mobile)
    w = 420 if mobile else 560
    narrow = 420 if mobile else 440
    invalid = form(s, STAFF, amount="1.00", term="9999", errors=INVALID_ERRORS, summary=SUMMARY)
    invalid += f'\n      <div style="{card(s, " padding-top: 4px;")}">{corr("4b1c9f2e7a3d5c81")}</div>'
    return {
        'SignIn': page(s, signin(s), width=narrow),
        'SignInRefused': page(s, signin(s, refused=True), width=narrow),
        'QuoteForm': page(s, form(s, STAFF), staff=STAFF, width=w),
        'QuoteInvalid': page(s, invalid, staff=STAFF, width=w),
        'QuoteSubmitting': page(s, form(s, STAFF, submitting=True), staff=STAFF, width=w),
        # The answer first, then what was asked. See collapsed_form.
        'Result': page(s, result_panel(s) + '\n' + collapsed_form(s, STAFF), staff=STAFF, width=w),
        'NotEntitled': page(s, not_entitled(s), staff="Sam Ellis", width=w),
        'Unavailable': page(s, form(s, STAFF) + '\n' + unavailable(s), staff=STAFF, width=w),
    }


if __name__ == '__main__':
    written = []
    for name, content in build(mobile=False).items():
        # The result is the state the application exists to reach, so it is Main.
        filename = 'Main.dc.html' if name == 'Result' else f'{name}.dc.html'
        open(filename, 'w').write(content)
        written.append(filename)

    # Phones get the four screens where the layout actually has to change.
    for name in ('SignIn', 'QuoteForm', 'QuoteInvalid', 'Result'):
        content = build(mobile=True)[name]
        filename = f'Phone{name}.dc.html'
        open(filename, 'w').write(content)
        written.append(filename)

    print('\n'.join(sorted(written)))
