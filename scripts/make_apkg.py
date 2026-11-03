"""Build anki/zhread.apkg: the zhread note type and no notes.

    python3 scripts/make_apkg.py

Importing the package into Anki once creates the note type, its four card
types and their styling from anki/. After that, cards.tsv imports directly:
its headers name the note type, the deck and the GUID column. Rebuild and
import the package again after changing a template; the note type keeps a
fixed ID, so Anki updates it rather than adding a second one.

Needs genanki: pip install -r requirements.txt.
"""

import os

import genanki

ANKI = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..",
                    "anki")

# Changing this makes Anki treat the note type as a new one.
MODEL_ID = 1761811200

# The note type's fields, in the order of cards.tsv's columns after the
# GUID, which cmd/zhread/card.go writes.
FIELDS = ["Simplified", "Traditional", "Pinyin", "DefZh", "DefEn",
          "Context", "Cloze", "ContextTrad", "ClozeTrad", "Source", "Added"]

# Anki shows a note's new cards in this order, so the traditional ones are
# learned first. Re-importing does not reorder a collection's card types:
# with "Merge note types" Anki aligns the incoming templates to the order
# it already has (Notetype::merge in rslib/src/notetype/merge.rs). A
# collection made with another order is changed in Anki itself, with
# Reposition in the note type's Cards screen.
CARD_TYPES = [
    ("Recognition (繁)", "recognition_trad"),
    ("Cloze (繁)", "cloze_trad"),
    ("Recognition (简)", "recognition_simp"),
    ("Cloze (简)", "cloze_simp"),
]


def read(name):
    with open(os.path.join(ANKI, name), encoding="utf-8") as f:
        return f.read()


def main():
    model = genanki.Model(
        MODEL_ID, "zhread",
        fields=[{"name": f} for f in FIELDS],
        templates=[{"name": name,
                    "qfmt": read(f + "_front.html"),
                    "afmt": read(f + "_back.html")}
                   for name, f in CARD_TYPES],
        css=read("style.css"))
    # Deck 1 is the Default deck every collection has; the package
    # creates no deck of its own, and cards.tsv names the one it uses.
    deck = genanki.Deck(1, "Default")
    deck.add_note(genanki.Note(model=model, guid="zhread-setup",
                               fields=["zhread setup"] + [""] * 10))
    out = os.path.join(ANKI, "zhread.apkg")
    genanki.Package(deck).write_to_file(out)
    print(out)


if __name__ == "__main__":
    main()
