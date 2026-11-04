"""Extract the Mandarin entries of English Wiktionary, as wiktextract
dumps them at kaikki.org, into wikt.tsv, zhread's second source of
English definitions after CC-CEDICT.

    curl -L \
        https://kaikki.org/dictionary/Chinese/kaikki.org-dictionary-Chinese.jsonl |
        python3 parse_wikt.py > wikt.tsv

The dump is read as a stream, one JSON record per line: it is over a
gigabyte, and only a few percent of it is kept.

Output columns: trad, simp, tw, cn, def, as in csld.tsv. One row per
record, which is a word with one part of speech and etymology. cn is the
Standard Mandarin reading, and tw a Taiwan reading where Wiktionary
tags one that differs. def is cells joined by ¶: the part of speech in
angle brackets, then each sense, each followed by its examples
("例：" text, translation) and at most one quotation ("引：" text,
translation, source).

Dropped: soft redirects (the simplified spellings, whose traditional
page holds the entry, and which supply the simplified column instead),
senses tagged or labelled only for other varieties of Chinese (Cantonese,
Hokkien and the rest) and examples in them, senses with no gloss, and records
left with no senses.
"""

import json
import os
import re
import sys

from parse_csld import inventory, split

# Varieties whose senses are dropped unless also tagged Mandarin.
TOPOLECTS = {
    "Cantonese", "Hokkien", "Teochew", "Hakka", "Wu", "Shanghainese",
    "Min", "Min-Nan", "Min-Bei", "Min-Dong", "Eastern-Min",
    "Northern-Min", "Southern-Min", "Puxian-Min", "Gan", "Xiang", "Jin",
    "Taishanese", "Hainanese", "Leizhou-Min", "Pinghua", "Dungan",
    "Hong-Kong-Cantonese", "Zhangzhou-Hokkien", "Xiamen", "Quanzhou",
    "Taiwanese-Hokkien", "Philippine-Hokkien", "Singaporean-Hokkien",
    "Penang-Hokkien", "Malaysian-Hokkien", "Suzhounese", "Wenzhounese",
}
# Romanisations of other varieties, tagged on their examples.
ROMANISATIONS = {
    "Jyutping", "Wugniu", "Pe̍h-ōe-jī", "Tâi-lô", "Phak-fa-su",
    "Hakka-Romanization-System", "Bàng-uâ-cê", "Cantonese-Pinyin",
    "Yale", "Kienning-Colloquial-Romanized",
}
# Topolect names as a gloss's leading label writes them: "(Shanghainese)",
# which some senses carry with no tag.
TOPOLECT_NAMES = {t.replace("-", " ") for t in TOPOLECTS}
LABEL = re.compile(r"^\(([^()]*)\)")
TONE_DIGITS = re.compile(r"[0-9¹²³⁴⁵⁶⁷⁸⁹⁰₀₁₂₃₄₅₆₇₈₉]")
MAX_EX = 3

HAN = re.compile(r"[㐀-鿿\U00020000-\U0003ffff]")


def squash(s):
    return re.sub(r"[\s'’-]", "", s.lower())


def clean(s):
    return re.sub(r"\s+", " ", s.replace("¶", " ")).strip()


def other_variety(gloss):
    """Whether a gloss's leading label names another variety of Chinese
    and not Mandarin: "(Shanghainese) currently"."""
    m = LABEL.match(gloss)
    if not m:
        return False
    names = set(re.split(r",\s*|\s+and\s+", m.group(1)))
    return bool(names & TOPOLECT_NAMES) and "Mandarin" not in m.group(1)


def mandarin(ex):
    """Whether an example is Mandarin: not tagged with another variety
    or its romanisation, and romanised, if at all, without the tone
    digits Jyutping and Wugniu use (hoeng¹, ⁶la)."""
    tags = set(ex.get("tags", [])) | set(ex.get("raw_tags", []))
    if tags & (TOPOLECTS | ROMANISATIONS):
        return False
    return not TONE_DIGITS.search(ex.get("roman", ""))


def bold_roman(rec):
    """The headword's reading as an example of this record gives it, in
    bold within the example's romanisation."""
    for s in rec.get("senses", []):
        for ex in s.get("examples", []):
            if not mandarin(ex):
                continue
            r, offs = ex.get("roman", ""), ex.get("bold_roman_offsets")
            if r and offs:
                return " ".join(r[a:b] for a, b in offs).lower()
    return ""


def syllabify(word, py, syl):
    """Split py into a syllable per character of word, an erhua 兒 being
    part of the syllable before it: 中不溜兒 is zhōng bù liūr. Fewer
    syllables are tried first, since a reading can often be forced into
    too many (z hōng bù liūr), but rarely into too few. None if no split
    fits."""
    n = len(HAN.findall(word))
    py = py.lower()
    for k in range(word.count("兒"), -1, -1):
        s = split(py, n - k, syl)
        if s is not None:
            return s
    return None


def readings(rec, syl):
    """The Standard Mandarin reading and a differing Taiwan one, split
    into syllables.

    A record's sounds are not its own: wiktextract lists those of the
    page's other etymologies with them, so 打 "dozen" carries 打 "hit"'s
    dǎ before its own dá. The record's reading is taken instead from
    the bold syllables of its first example, and only failing that from
    the first standard reading listed. A Taiwan reading is noted only
    where the sounds tag a Mainland reading that is the one chosen and a
    different Taiwan one: dá "standard in Mainland China", dǎ "standard
    in Taiwan"."""
    first = mainland = taiwan = ""
    for s in rec.get("sounds", []):
        tags = set(s.get("tags", []))
        if not {"Mandarin", "Pinyin"} <= tags or "zh_pron" not in s:
            continue
        if not tags & {"Standard", "Standard-Chinese"}:
            continue
        # A reading tagged phonetic spells out tone sandhi (lífǎ after
        # 理髮's Taiwan lǐfǎ); one may also carry it after the reading,
        # unclosed: "bùkānyījí [Phonetic: bùkānyìjí".
        if "phonetic" in tags:
            continue
        py = re.sub(r"\s*\[Phonetic:.*$", "", s["zh_pron"])
        py = re.sub(r"\s*\(.*?\)\s*$", "", py).strip()
        first = first or py
        if "Mainland-China" in tags:
            mainland = py
        elif "Taiwan" in tags and "variant" not in tags:
            taiwan = py
    cn = bold_roman(rec) or first
    tw = ""
    if mainland and taiwan and squash(mainland) == squash(cn):
        tw = taiwan

    def sp(py):
        if not py or " " in py:
            return py
        return syllabify(rec["word"], py, syl) or py

    cn, tw = sp(cn), sp(tw)
    if squash(tw) == squash(cn):
        tw = ""
    return tw, cn


def example(ex):
    text = clean(ex.get("text", ""))
    if not text:
        return ""
    tr = clean(ex.get("english") or ex.get("translation") or "")
    out = text + (" " + tr if tr else "")
    if ex.get("type") == "quotation":
        ref = clean(ex.get("ref", ""))
        return "引：" + out + (" (" + ref + ")" if ref else "")
    return "例：" + out


def senses(rec):
    cells = []
    for s in rec.get("senses", []):
        tags = set(s.get("tags", []))
        if "no-gloss" in tags:
            continue
        if tags & TOPOLECTS and "Mandarin" not in tags:
            continue
        gl = s.get("raw_glosses") or s.get("glosses")
        if not gl:
            continue
        # A sub-sense's glosses start with its parent's, which is a
        # sense of its own too.
        g = clean(gl[-1])
        if other_variety(g):
            continue
        # As in "(～市) Kyoto", the headword in a compound.
        cells.append(g.replace("～", rec["word"]))
        n, quoted = 0, False
        for ex in s.get("examples", []):
            # Each example is given in both scripts; one will do.
            if ("Simplified-Chinese" in ex.get("tags", []) or
                    not mandarin(ex)):
                continue
            e = example(ex)
            if not e:
                continue
            if e.startswith("引："):
                if quoted:
                    continue
                quoted = True
            elif n >= MAX_EX:
                continue
            else:
                n += 1
            cells.append(e)
    return cells


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    syl = inventory(f"{here}/cedict_parsed.tsv")
    simp_of = {}
    rows = []
    for line in sys.stdin:
        try:
            rec = json.loads(line)
        except json.JSONDecodeError:
            continue
        if rec.get("lang_code") != "zh":
            continue
        word = rec.get("word", "")
        if rec.get("pos") == "soft-redirect":
            for t in rec.get("redirects", []):
                simp_of.setdefault(t, word)
            continue
        if not HAN.search(word):
            continue
        cells = senses(rec)
        if not cells:
            continue
        simp = ""
        for f in rec.get("forms", []):
            if "Simplified-Chinese" in f.get("tags", []):
                simp = f["form"]
                break
        tw, cn = readings(rec, syl)
        rows.append([word, simp, tw, cn,
                     "¶".join(["<" + rec.get("pos", "") + ">"] + cells)])
    print("trad\tsimp\ttw\tcn\tdef")
    for r in rows:
        r[1] = r[1] or simp_of.get(r[0], r[0])
        print("\t".join(c.replace("\t", " ") for c in r))


if __name__ == "__main__":
    main()
