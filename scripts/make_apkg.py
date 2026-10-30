"""Build an Anki package from zhread's cards.tsv.

    python3 scripts/make_apkg.py [cards.tsv [cards.apkg]]

The package carries the zhread note type, its four card types and their
styling from anki/, so importing it into Anki needs no setup beforehand.
Every note keeps the GUID zhread gave it, and the note type and deck keep
fixed IDs, so importing a newer package updates the notes already in Anki
rather than adding them again.

Needs genanki: pip install -r requirements.txt.
"""

import csv
import hashlib
import os
import sys

import genanki

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
ANKI = os.path.join(ROOT, "anki")

# Changing this makes Anki treat the note type as a new one.
MODEL_ID = 1761811200

CARD_TYPES = [
    ("Recognition (简)", "recognition_simp"),
    ("Cloze (简)", "cloze_simp"),
    ("Recognition (繁)", "recognition_trad"),
    ("Cloze (繁)", "cloze_trad"),
]


def read(name):
    with open(os.path.join(ANKI, name), encoding="utf-8") as f:
        return f.read()


def read_cards(path):
    """The import headers zhread writes, as a dict, and the rows."""
    headers, lines = {}, []
    with open(path, encoding="utf-8", newline="") as f:
        for line in f:
            if line.startswith("#"):
                k, _, v = line[1:].rstrip("\n").partition(":")
                headers[k] = v
            else:
                lines.append(line)
    return headers, list(csv.reader(lines, delimiter="\t"))


def main():
    args = sys.argv[1:] + [None, None]
    src = args[0] or os.path.join(ROOT, "cards.tsv")
    out = args[1] or os.path.splitext(src)[0] + ".apkg"
    if not os.path.exists(src):
        sys.exit(f"{src}: no cards yet; make some with zhread first")
    headers, rows = read_cards(src)
    # The first column is the GUID; the rest are the note's fields.
    fields = headers["columns"].split("\t")[1:]
    model = genanki.Model(
        MODEL_ID, headers.get("notetype", "zhread"),
        fields=[{"name": f} for f in fields],
        templates=[{"name": name,
                    "qfmt": read(f + "_front.html"),
                    "afmt": read(f + "_back.html")}
                   for name, f in CARD_TYPES],
        css=read("style.css"))
    name = headers.get("deck", "Chinese reading")
    deck_id = int(hashlib.sha1(name.encode()).hexdigest()[:12], 16)
    deck = genanki.Deck(deck_id, name)
    for r in rows:
        if len(r) != len(fields) + 1:
            sys.exit(f"{src}: a row has {len(r)} columns, "
                     f"want {len(fields) + 1}: {r[:2]}")
        deck.add_note(genanki.Note(model=model, fields=r[1:], guid=r[0]))
    genanki.Package(deck).write_to_file(out)
    print(f"{out}: {len(rows)} notes, "
          f"{sum(len(n.cards) for n in deck.notes)} cards")


if __name__ == "__main__":
    main()
