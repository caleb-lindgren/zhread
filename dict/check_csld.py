"""Check the cross-strait dictionary's pinyin against its own Zhuyin, and
list the readings where they disagree that csld_fixes.tsv does not cover:

    python3 check_csld.py 兩岸詞典.csv

Each line is trad, side (tw or cn), the pinyin, and the Zhuyin as
pinyin. Run it on a new download of the source and review what it lists
into csld_fixes.tsv: a corrected reading where the pinyin is wrong, or
the pinyin unchanged where it is the Zhuyin that is (千里姻緣一線牽 has
ㄩˊ for 緣) or the two only write erhua differently (勁兒 jìnr against
ㄐㄧㄣˋ ㄦ). The Zhuyin is not taken as the correction automatically for
that reason.

Readings whose Zhuyin is not standard (Taiwan's spoken erhua finals, as
ㄏㄨㄜˇㄦ for 會兒, and look-alike characters such as ｜ for ㄧ) are
counted, not checked.
"""

import csv
import os
import re
import sys
import unicodedata

from parse_csld import SIDES, load_fixes

INITIALS = dict(zip("ㄅㄆㄇㄈㄉㄊㄋㄌㄍㄎㄏㄐㄑㄒㄓㄔㄕㄖㄗㄘㄙ",
    "b p m f d t n l g k h j q x zh ch sh r z c s".split()))
# Finals as written after an initial, and on their own.
FINALS = {
    "": ("i", None), "ㄚ": ("a", "a"), "ㄛ": ("o", "o"), "ㄜ": ("e", "e"),
    "ㄝ": ("ê", "ê"), "ㄞ": ("ai", "ai"), "ㄟ": ("ei", "ei"),
    "ㄠ": ("ao", "ao"), "ㄡ": ("ou", "ou"), "ㄢ": ("an", "an"),
    "ㄣ": ("en", "en"), "ㄤ": ("ang", "ang"), "ㄥ": ("eng", "eng"),
    "ㄦ": (None, "er"),
    "ㄧ": ("i", "yi"), "ㄧㄚ": ("ia", "ya"), "ㄧㄛ": ("io", "yo"),
    "ㄧㄝ": ("ie", "ye"), "ㄧㄞ": ("iai", "yai"), "ㄧㄠ": ("iao", "yao"),
    "ㄧㄡ": ("iu", "you"), "ㄧㄢ": ("ian", "yan"), "ㄧㄣ": ("in", "yin"),
    "ㄧㄤ": ("iang", "yang"), "ㄧㄥ": ("ing", "ying"),
    "ㄨ": ("u", "wu"), "ㄨㄚ": ("ua", "wa"), "ㄨㄛ": ("uo", "wo"),
    "ㄨㄞ": ("uai", "wai"), "ㄨㄟ": ("ui", "wei"), "ㄨㄢ": ("uan", "wan"),
    "ㄨㄣ": ("un", "wen"), "ㄨㄤ": ("uang", "wang"),
    "ㄨㄥ": ("ong", "weng"),
    "ㄩ": ("ü", "yu"), "ㄩㄝ": ("üe", "yue"), "ㄩㄢ": ("üan", "yuan"),
    "ㄩㄣ": ("ün", "yun"), "ㄩㄥ": ("iong", "yong"),
}
TONES = {"": "̄", "ˊ": "́", "ˇ": "̌", "ˋ": "̀"}
# The erhua ㄦ comes before the tone mark or after it (ㄅㄢˋㄦ).
SYL = re.compile(r"(˙?)([ㄅ-ㄙ]?)([ㄚ-ㄩ]*?)(ㄦ?)([ˊˇˋ]?)(ㄦ?)")


def mark(py, tone):
    """Put the tone mark where pinyin puts it: on a or e, on the o of
    ou, else on the last vowel."""
    for v in ("a", "e", "ê", "o" if "ou" in py else None):
        if v and v in py:
            i = py.index(v)
            break
    else:
        i = max(py.rfind(v) for v in "iouü")
    return unicodedata.normalize("NFC", py[:i + 1] + tone + py[i + 1:])


def syllable(z):
    """One Zhuyin syllable in pinyin, or None."""
    m = SYL.fullmatch(z)
    if not m:
        return None
    neutral, ini, fin, er, tone, er2 = m.groups()
    if er and er2:
        return None
    er = er or er2
    if not ini and not fin and er:
        fin, er = "ㄦ", ""
    if fin not in FINALS:
        return None
    after, alone = FINALS[fin]
    if ini:
        if after is None or (fin == "" and ini not in "ㄓㄔㄕㄖㄗㄘㄙ"):
            return None
        if ini in "ㄐㄑㄒ":
            after = after.replace("ü", "u")
        py = INITIALS[ini] + after
    else:
        if alone is None:
            return None
        py = alone
    if not neutral:
        py = mark(py, TONES[tone])
    return py + ("r" if er else "")


def to_pinyin(zhuyin):
    """A Zhuyin reading in pinyin, a list of syllables, or None if any
    syllable is not one. 丨 (U+4E28), which the cross-strait dictionary
    writes for ㄧ, is read as ㄧ."""
    out = []
    for s in re.split(r"[　\s，、；：。！？]+", zhuyin.replace("丨", "ㄧ").strip()):
        if not s:
            continue
        p = syllable(s)
        if p is None:
            return None
        out.append(p)
    return out


def squash(py):
    """Pinyin with the source's notation removed: syllable separators, ɡ
    and ɑ for g and a, and case."""
    py = py.replace("ɡ", "g").replace("ɑ", "a").lower()
    return unicodedata.normalize("NFC", re.sub(r"[\s\-–－,，’'·]", "", py))


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    fixes = load_fixes(here)
    checked = unparsed = listed = 0
    with open(sys.argv[1], encoding="utf-8") as f:
        for r in csv.DictReader(f):
            trad = r["正體字形"].strip()
            for side, name in SIDES.items():
                zhuyin = r[name + "音讀"].strip()
                py = r[name + "漢拼"].strip()
                if not zhuyin or not py:
                    continue
                conv = to_pinyin(zhuyin)
                if conv is None:
                    unparsed += 1
                    continue
                checked += 1
                if squash("".join(conv)) == squash(py):
                    continue
                fix = fixes.get((trad, side))
                if fix and fix[0] == py:
                    continue
                listed += 1
                print(f"{trad}\t{side}\t{py}\t{' '.join(conv)}")
    print(f"{checked} readings checked, {unparsed} with Zhuyin not read, "
          f"{listed} listed", file=sys.stderr)


if __name__ == "__main__":
    main()
