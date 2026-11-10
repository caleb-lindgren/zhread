"""Make zhread-cards.gif: the cards of a card file as Anki shows them,
each card's question and then its answer.

    python3 scripts/cards_demo.py [--light] CARDS [OUT]

CARDS is a card file, as `scripts/zhread_demo.sh` saves the demo's with
CARDS=FILE; OUT defaults to zhread-cards.gif. Each note's cards are those
Anki would make of it, in the note type's order: a card type whose front
comes out empty, as the simplified ones do for a note from a traditional
text, makes no card. The answer of the first card is shown twice, the
second time with its English definitions opened, as a click on "English"
opens them.

The cards are shown in Anki's dark theme, which the styling, setting no
colors but the word's Zenburn orange, is made for; --light shows them in
its light theme. The themes' colors here are approximations of Anki's:
the background and the text. The card's styling centers it with no margin
of its own (.card is the body), so the page pads it 20px at the sides, as
Anki's window does.

The templates and styling are anki/'s, filled in as Anki fills them in
(fields, {{text:Field}}, {{#Field}} and {{^Field}} sections, {{Card}}),
and drawn by Chromium through Playwright, so the cards look as they do in
Anki's card view, without the rest of its window. The fonts are the ones
the styling names, as installed: TW-MOE-Std-Kai and a serif with
simplified Chinese (Source Han Serif or Noto Serif CJK SC) for the cards
to look as intended. Needs requirements-demo.txt and a Chromium for
Playwright (`.venv/bin/playwright install chromium`); `make cards-gif`
sets up both.
"""

import html
import io
import os
import re
import sys

from PIL import Image
from playwright.sync_api import sync_playwright

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
sys.path.insert(0, os.path.join(ROOT, "scripts"))
from make_apkg import CARD_TYPES, FIELDS, read  # noqa: E402

WIDTH = 1000          # pixels, the card view's width
QUESTION_MS = 2500
ANSWER_MS = 5000

# Anki's card view, before the note type's styling: its theme's colors,
# and padding at the sides of the page.
BASE = ("html {{ padding: 0 20px 20px; background: {0}; }} "
        "body {{ background: {0}; color: {1}; }}")
THEMES = {"dark": ("#2c2c2c", "#fcfcfc"), "light": ("#ffffff", "#000000")}

# A strip above the card naming what is shown, as zhread.gif's captions
# do; outside .card, so the card's own styling is untouched.
BANNER = """<div style="position: sticky; top: 0; margin: 0 -20px 24px;
padding: 10px 24px; background: #3a3a3a; color: #f0dfaf;
font: 26px/1.4 'Liberation Sans', arial, sans-serif;">{}</div>"""


def load(path):
    """The notes of a card file, each a dict of the note type's fields."""
    notes = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            cols = line.split("\t")
            notes.append(dict(zip(FIELDS, cols[1:] + [""] * len(FIELDS))))
    return notes


def empty(value):
    """Anki's test for an empty field: nothing left once tags, &nbsp; and
    white space are gone."""
    return not re.sub(r"<[^>]*>|&nbsp;|\s", "", value)


def text(value):
    """{{text:Field}}: the field with its HTML removed."""
    return html.unescape(re.sub(r"<[^>]*>", "", value))


TAG = re.compile(r"{{([#^/]?)([^{}]+?)}}")


def render(tmpl, note, card):
    """Fill in a template as Anki does."""
    out, pos, stack = [], 0, []
    # stack holds (field, kind, whether its content is shown, out length)
    shown = True
    for m in TAG.finditer(tmpl):
        if shown:
            out.append(tmpl[pos:m.start()])
        pos = m.end()
        kind, name = m.group(1), m.group(2).strip()
        if kind in "#^" and kind:
            on = not empty(note.get(name, ""))
            stack.append((name, shown))
            shown = shown and (on if kind == "#" else not on)
        elif kind == "/":
            open_name, shown = stack.pop()
            if open_name != name:
                raise ValueError(f"{{{{/{name}}}}} closes {open_name}")
        elif shown:
            if name == "Card":
                out.append(card)
            elif name.startswith("text:"):
                out.append(text(note.get(name[5:], "")))
            else:
                out.append(note.get(name, ""))
    if stack:
        raise ValueError(f"{{{{#{stack[-1][0]}}}}} is not closed")
    if shown:
        out.append(tmpl[pos:])
    return "".join(out)


def page(css, body, caption, theme):
    classes = "card card1" + (" nightMode night_mode"
                              if theme == "dark" else "")
    return (f"<!doctype html><meta charset=utf-8>"
            f"<style>{BASE.format(*THEMES[theme])}</style>"
            f"<style>{css}</style>"
            f'<body class="{classes}">{BANNER.format(caption)}'
            f"{body}</body>")


def shoot(pg, doc, open_english=False):
    pg.set_content(doc)
    pg.evaluate("document.fonts.ready")
    if open_english:
        pg.evaluate("document.querySelectorAll('details')"
                    ".forEach(d => d.open = true)")
    height = pg.evaluate("document.documentElement.scrollHeight")
    pg.set_viewport_size({"width": WIDTH, "height": height})
    return Image.open(io.BytesIO(pg.screenshot())).convert("RGB")


def main():
    args = sys.argv[1:]
    theme = "dark"
    if args[:1] == ["--light"]:
        theme, args = "light", args[1:]
    if len(args) not in (1, 2):
        sys.exit(__doc__)
    out = args[1] if len(args) == 2 else os.path.join(
        ROOT, "zhread-cards.gif")
    css = read("style.css")
    frames = []  # (image, milliseconds)
    with sync_playwright() as p:
        browser = p.chromium.launch()
        pg = browser.new_page(viewport={"width": WIDTH, "height": 600})
        for note in load(args[0]):
            word = note["Traditional"] or note["Simplified"]
            for name, base in CARD_TYPES:
                q = render(read(base + "_front.html"), note, name)
                if empty(re.sub(r"<script.*?</script>", "", q, flags=re.S)):
                    continue
                a = render(read(base + "_back.html"), note, name)
                kind = name.split(" ")[0].lower()
                def add(side, caption, ms, **kw):
                    doc = page(css, side, f"{name}: {caption}", theme)
                    frames.append((shoot(pg, doc, **kw), ms))
                add(q, "the question", QUESTION_MS)
                add(a, f"the answer, {word}", ANSWER_MS)
                if kind == "recognition" and "<details" in a:
                    add(a, "the answer, English opened", ANSWER_MS,
                        open_english=True)
        browser.close()
    if not frames:
        sys.exit(f"{args[0]}: no cards")
    # One size for every frame: the tallest, the rest padded below in the
    # card's background.
    h = max(im.height for im, _ in frames)
    imgs = []
    for im, _ in frames:
        ground = im.getpixel((WIDTH - 1, im.height - 1))
        pad = Image.new("RGB", (WIDTH, h), ground)
        pad.paste(im, (0, 0))
        imgs.append(pad.quantize(colors=256, method=Image.Quantize.MEDIANCUT))
    imgs[0].save(out, save_all=True, append_images=imgs[1:], loop=0,
                 duration=[ms for _, ms in frames], optimize=True)
    print(f"wrote {out}: {len(frames)} frames, {WIDTH}x{h}")


if __name__ == "__main__":
    main()
