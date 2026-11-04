"""Extract Taiwan/Mainland reading differences from the cross-strait
dictionary (兩岸詞典.csv) into csld_pron.tsv.

    python3 parse_csld.py 兩岸詞典.csv > csld_pron.tsv

Output columns: trad, simp, tw, cn. Pinyin is written one space between
syllables, as CC-CEDICT and MoEDict write it. Only rows whose two readings
differ in pronunciation are kept, not those differing only in notation:
the Taiwan column writes the tone sandhi of 一 and 不 (一人 yìrén) and
erhua as spoken (一點兒 yìdiǎr), where the Mainland column writes the base
forms (yīrén, yīdiǎnr).

The source's pinyin has typos (坑人 kēngrēn, 奎 kúi, 呼牛呼馬 hūniú-hūm),
corrected from csld_fixes.tsv; see pinyin() and check_csld.py.
"""

import csv
import os
import re
import sys
import unicodedata

TONES = "̀́̄̌"  # grave, acute, macron, caron


def toneless(s):
    d = unicodedata.normalize("NFD", s)
    d = "".join(c for c in d if c not in TONES)
    return unicodedata.normalize("NFC", d)


def inventory(cedict):
    """Every toneless syllable CC-CEDICT uses, plus its erhua form."""
    syl = set()
    with open(cedict, encoding="utf-8") as f:
        next(f)
        for line in f:
            for t in line.split("\t")[2].split():
                t = toneless(t.lower().replace("u:", "ü"))
                t = t.rstrip("12345")
                if t.isalpha():
                    syl.add(t)
    # 崖 is yái in Taiwan, a syllable CC-CEDICT never uses.
    syl.add("yai")
    # Erhua is spelled either by appending r to the syllable (Mainland,
    # diǎnr) or as spoken, with a final -n or -i dropped (Taiwan, diǎr,
    # and huěr for huìr).
    er = {s + "r" for s in syl}
    for s in syl:
        if len(s) > 1 and s[-1] in "ni":
            er |= {s[:-1] + "r", s[:-1] + "er"}
    return syl | er


def syllables(zhuyin):
    """Count the syllables of a Zhuyin reading, which separates them with
    ideographic spaces and keeps the headword's punctuation."""
    sep = r"[　\s，、；：。！？]+"
    return len([t for t in re.split(sep, zhuyin) if t])


def split(py, n, syl):
    """Split pinyin into exactly n syllables, treating spaces, hyphens,
    commas and apostrophes as forced boundaries. None if impossible."""
    pieces = [p for p in re.split(r"[\s\-–,，’']+", py) if p]
    flat = "".join(pieces)
    plain = toneless(flat)
    if len(plain) != len(flat):
        return None
    forced, i = set(), 0
    for p in pieces:
        i += len(p)
        forced.add(i)
    # best[i][k]: a split of flat[:i] into k syllables exists
    best = [dict() for _ in range(len(flat) + 1)]
    best[0][0] = None
    for i in range(len(flat)):
        if not best[i]:
            continue
        for j in range(i + 1, min(len(flat), i + 7) + 1):
            if plain[i:j] not in syl:
                continue
            if any(i < b < j for b in forced):
                continue
            for k in best[i]:
                best[j].setdefault(k + 1, i)
    if n not in best[len(flat)]:
        return None
    out, j, k = [], len(flat), n
    while j:
        i = best[j][k]
        out.append(flat[i:j])
        j, k = i, k - 1
    return " ".join(reversed(out))


def notational(trad, tw, cn):
    """True when the readings differ only by 一/不 sandhi or by how erhua
    is spelled."""
    if len(tw) != len(cn):
        return False
    han = [c for c in trad if unicodedata.category(c) == "Lo"]
    for i, (a, b) in enumerate(zip(tw, cn)):
        if a == b:
            continue
        if i < len(han) and han[i] in "一不" and toneless(a) == toneless(b):
            continue
        if a.endswith("r") and b.endswith("nr") and a[:-1] == b[:-2]:
            continue
        return False
    return True


SIDES = {"tw": "臺灣", "cn": "大陸"}


def load_fixes(here):
    """csld_fixes.tsv, keyed on (trad, side): the source's pinyin and the
    reading to use instead."""
    fixes = {}
    with open(f"{here}/csld_fixes.tsv", encoding="utf-8") as f:
        next(f)
        for line in f:
            trad, side, source, fixed, _ = line.rstrip("\n").split("\t")
            fixes[(trad, side)] = (source, fixed)
    return fixes


def pinyin(r, side, fixes):
    """The row's pinyin for side ("tw" or "cn"), corrected where
    csld_fixes.tsv has the word. A fix applies only to the reading it
    was made against: a word can have several rows (酇 cuó and zàn), and
    a source corrected upstream is not overwritten. check_csld.py lists
    a typo whose fix no longer applies."""
    trad = r["正體字形"].strip()
    py = r[SIDES[side] + "漢拼"].strip()
    fix = fixes.get((trad, side))
    if fix is None or fix[0] != py:
        return py
    return fix[1]


def main():
    src = sys.argv[1]
    here = os.path.dirname(os.path.abspath(__file__))
    syl = inventory(f"{here}/cedict_parsed.tsv")
    fixes = load_fixes(here)
    w = csv.writer(sys.stdout, delimiter="\t", lineterminator="\n")
    w.writerow(["trad", "simp", "tw", "cn"])
    skipped = 0
    with open(src, encoding="utf-8") as f:
        for r in csv.DictReader(f):
            cn = pinyin(r, "cn", fixes)
            if not cn:
                continue
            # A reading the Mainland has and Taiwan lacks pairs with
            # nothing.
            if not r["臺灣漢拼"].strip():
                continue
            trad = r["正體字形"].strip()
            simp = r["簡化字形"].strip() or trad
            fix = lambda s: s.replace("ɡ", "g").replace("ɑ", "a").lower()
            tw_n = syllables(r["臺灣音讀"])
            cn_n = syllables(r["大陸音讀"])
            tw = split(fix(pinyin(r, "tw", fixes)), tw_n, syl)
            cnp = split(fix(cn), cn_n, syl)
            if tw is None or cnp is None:
                skipped += 1
                print(f"skipped {trad}: {r['臺灣漢拼']} / {cn}",
                      file=sys.stderr)
                continue
            if notational(trad, tw.split(), cnp.split()):
                continue
            w.writerow([trad, simp, tw, cnp])
    print(f"{skipped} rows could not be split into syllables",
          file=sys.stderr)


if __name__ == "__main__":
    main()
