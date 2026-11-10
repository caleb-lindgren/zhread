"""Apply the hand corrections to the Pleco MoEDict export, in place.

    python3 fix_moedict.py MoEDICT/MoEDict-05-Simp03-cards.txt

Each correction names an entry by its headword and reading fields, the
first two tab-separated fields of a line, and gives what they become; the
definition is left alone. They are:

- Six characters and words (尴, 尴尬, 弄, 脚, 血, 遍) whose reading carries
  the source's label for a variant reading, as " (語音) xiě" for 血, or a
  second reading after the first, as 尴尬's "gān gà  (又音) jiān jiè". The
  label, or the second reading, is dropped. The export's other 612
  labelled readings are left as they are.
- 般, whose simplified headword the export gives as 举.

A correction whose entry is not found, or is found more often than
listed, is an error: the export has changed, and the list needs review.
"""

import sys

# (headword, reading) as in the export, the same after the correction, and
# how many lines have that headword and reading.
FIXES = [
    (("尴[尷]", " (又音) jiān"), ("尴[尷]", " jiān"), 1),
    (("尴尬[尷尬]", "gān gà  (又音) jiān jiè"), ("尴尬[尷尬]", "gān gà"), 1),
    (("弄[弄]", " (語音) nòng"), ("弄[弄]", "nòng"), 1),
    (("弄[弄]", " (讀音) lòng"), ("弄[弄]", "lòng"), 1),
    (("脚[腳]", " (語音) jiǎo"), ("脚[腳]", "jiǎo"), 1),
    (("脚[腳]", " (讀音) jué"), ("脚[腳]", "jué"), 1),
    (("举[般]", "bān"), ("般[般]", "bān"), 1),
    (("举[般]", "bō"), ("般[般]", "bō"), 1),
    (("举[般]", "pán"), ("般[般]", "pán"), 1),
    (("血[血]", " (語音) xiě"), ("血[血]", "xiě"), 1),
    (("血[血]", " (讀音) xuè"), ("血[血]", "xuè"), 1),
    (("遍[遍]", " (語音) piàn"), ("遍[遍]", "piàn"), 1),
    (("遍[遍]", " (讀音) biàn"), ("遍[遍]", "biàn"), 1),
]


def main():
    path = sys.argv[1]
    # newline="" keeps the export's line endings as they are.
    with open(path, encoding="utf-8", newline="") as f:
        lines = f.read().split("\n")
    fixes = {old: new for old, new, _ in FIXES}
    found = dict.fromkeys(fixes, 0)
    for i, line in enumerate(lines):
        f = line.split("\t", 2)
        key = tuple(f[:2])
        if len(f) == 3 and key in fixes:
            found[key] += 1
            lines[i] = "\t".join([*fixes[key], f[2]])
    bad = [(old, found[old], n) for old, _, n in FIXES if found[old] != n]
    for (head, reading), got, want in bad:
        print(f"{path}: {head} {reading!r}: {got} lines, expected {want}",
              file=sys.stderr)
    if bad:
        sys.exit(1)
    with open(path, "w", encoding="utf-8", newline="") as f:
        f.write("\n".join(lines))


if __name__ == "__main__":
    main()
