"""Extract the definitions of the cross-strait dictionary (兩岸詞典.csv)
into csld.tsv, zhread's first source of Chinese definitions.

    python3 parse_csld_defs.py 兩岸詞典.csv > csld.tsv

Output columns: trad, simp, tw, cn, def. tw and cn are the Taiwan and
Mainland readings, one space between syllables as in csld_pron.tsv; cn is
empty where the source gives no separate Mainland reading, which is where
the two are the same, and tw is empty for the few words with a Mainland
reading only. Fields are written unquoted. def is the definition cells
of the row joined by ¶, which the source never uses: a cell ending in ：
heads the cells after it (1.擊打： over (1), (2) ...). Each sense's
examples, which the source puts after [例] and separates with any of
four kinds of bar (∣ │ ︱ ｜), are a cell after it in MoEDict's form,
如：「…」、「…」。; quotations among them are cells of their own after
引：. The source's ～ for the headword is replaced by the headword. A word the source marks as used only on the Mainland (★)
or in Taiwan (▲) has the mark as a cell of its own before the others.
"""

import csv
import os
import re
import sys

from parse_csld import inventory, split, syllables

BARS = str.maketrans({"∣": "｜", "│": "｜", "︱": "｜"})
GLOSS = re.compile(r"(（[^（）]*）)$")
# A quotation ends with its source: （唐‧李白〈猛虎行〉）, the book in 《》
# or the piece in 〈〉, and a few with an ASCII bracket.
CITE = re.compile(r"[（(][^（）()]*[《〈][^（）()]*[）)]。?$")
# A heading run together with its first sub-sense: 1.地名： (1)晉代指….
HEAD = re.compile(r"^(.*?：)\s*(?=\(1\))")


def quote(e):
    """An example in corner brackets, as MoEDict writes them, with any
    brackets inside it made double and a gloss after it kept outside:
    的證（確鑿的證據） is 「的證」（確鑿的證據）."""
    e = e.replace("「", "『").replace("」", "』")
    m = GLOSS.search(e)
    if m and m.start() > 0:
        return "「" + e[:m.start()].removesuffix("。") + "」" + m.group(1)
    return "「" + e + "」"


def examples(ex):
    """The cells for what follows [例]: the examples as one cell in
    MoEDict's form, 如：「…」、「…」。, then each quotation, one that ends
    with its source in 《》, as a cell of its own after 引：. The source
    ends each list with 。, which is dropped from the last example and put
    after the list instead, unless the example ends with ？ or ！."""
    exs, cites = [], []
    for e in ex.split("｜"):
        e = e.strip()
        if CITE.search(e):
            cites.append("引：" + e)
        elif e:
            exs.append(e)
    cells = []
    if exs:
        exs[-1] = exs[-1].removesuffix("。")
        end = "" if exs[-1][-1] in "？！?!" else "。"
        cells.append("如：" + "、".join(map(quote, exs)) + end)
    return cells + cites


def reading(py, zhuyin, syl):
    """The reading split into syllables, or as written if it cannot be."""
    py = py.strip().replace("ɡ", "g").replace("ɑ", "a").lower()
    if not py:
        return ""
    return split(py, syllables(zhuyin), syl) or py


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    syl = inventory(f"{here}/cedict_parsed.tsv")
    print("trad\tsimp\ttw\tcn\tdef")
    with open(sys.argv[1], encoding="utf-8") as f:
        for r in csv.DictReader(f):
            trad = r["正體字形"].strip()
            if not trad:
                continue
            simp = r["簡化字形"].strip() or trad
            cells = []
            for i in range(1, 31):
                c = r.get("釋義" + str(i).translate(
                    str.maketrans("0123456789", "０１２３４５６７８９")))
                c = re.sub(r"\s+", " ", (c or "").translate(BARS)).strip()
                if not c:
                    continue
                # The source writes the headword as ～.
                c = c.replace("¶", " ").replace("～", trad).replace(
                    "〜", trad)
                # A few cells have [例] more than once.
                text, *ex = c.split("[例]")
                ex = "｜".join(ex)
                if m := HEAD.match(text):
                    cells.append(m.group(1))
                    text = text[m.end():]
                if text.strip():
                    cells.append(text.strip())
                cells += examples(ex)
            if not cells:
                continue
            mark = r["臺／陸特有詞"].strip()
            if mark:
                cells.insert(0, mark)
            tw = reading(r["臺灣漢拼"], r["臺灣音讀"], syl)
            cn = reading(r["大陸漢拼"], r["大陸音讀"], syl)
            print("\t".join([trad, simp, tw, cn, "¶".join(cells)]))


if __name__ == "__main__":
    main()
