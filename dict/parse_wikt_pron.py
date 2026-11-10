"""Collect the Taiwan and Mainland reading pairs of wikt.tsv into
wikt_pron.tsv, which zhread consults after csld_pron.tsv:

    python3 parse_wikt_pron.py > wikt_pron.tsv

Run it after parse_wikt.py and, if csld_pron.tsv is wanted, after
parse_csld.py, since it reads both outputs. The columns are those of csld_pron.tsv: trad, simp, tw, cn,
lower case, one space between syllables.

A word csld_pron.tsv already has is left out, so the cross-strait
dictionary decides wherever it has a pair; where both have the word they
mostly agree, and where they do not it is usually Wiktionary that writes
another sense (堤 tí/shí rather than tí/dī). So are pairs differing only in
notation, as in csld_pron.tsv, and readings that cannot be split into one
syllable per character.
"""

import os
import re
import sys

from parse_csld import inventory, notational
from parse_wikt import syllabify


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    syl = inventory(f"{here}/cedict_parsed.tsv")
    csld = set()
    # Without csld_pron.tsv (an optional extra), every pair is kept.
    if os.path.exists(f"{here}/csld_pron.tsv"):
        with open(f"{here}/csld_pron.tsv", encoding="utf-8") as f:
            for line in f:
                csld.add(line.split("\t", 1)[0])
    seen = set()
    rows = []
    unsplit = 0
    with open(f"{here}/wikt.tsv", encoding="utf-8") as f:
        next(f)
        for line in f:
            trad, simp, tw, cn, _ = line.split("\t", 4)
            if not tw or trad in csld:
                continue
            tw_s, cn_s = (syllabify(trad, re.sub(r"['’]", " ", s), syl)
                          for s in (tw, cn))
            if tw_s is None or cn_s is None:
                unsplit += 1
                continue
            if tw_s == cn_s or notational(trad, tw_s.split(), cn_s.split()):
                continue
            key = (trad, tw_s, cn_s)
            if key in seen:
                continue
            seen.add(key)
            rows.append([trad, simp or trad, tw_s, cn_s])
    print("trad\tsimp\ttw\tcn")
    for r in rows:
        print("\t".join(r))
    print(f"{len(rows)} pairs; {unsplit} not split into syllables",
          file=sys.stderr)


if __name__ == "__main__":
    main()
