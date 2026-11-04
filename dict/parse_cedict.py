import pandas as pd
import sys
import re

from dragonmapper import transcriptions

cedict = pd.read_csv(sys.argv[1], sep="\t", skiprows=range(30), header=None)[0].str.split(" ", n=2, expand=True)
cedict.columns = ["trad", "simp", "pydef"]

# Split out pinyin and definitions
pydef_split = cedict["pydef"].\
str[1:].\
str.split("] ", n=1, expand=True)

cedict = cedict.\
drop(columns="pydef").\
assign(**{
    "py": pydef_split[0],
    "def": pydef_split[1],
})

# Format definitions
cedict = cedict.assign(**{
    "def": cedict["def"].str[1:-1].str.replace("/", "; ")
})

# Convert numbered tones to tone marks
# CC-CEDICT writes ü as u: (lu:4, nu:e4), which dragonmapper misreads
# (lù:), so it is turned into ü first.
pinyin_match = r"\b[A-Za-z](?:u:|[a-z])*[1-5]\b"
def pinyin_replace_nums(x):
    syl = x[0].replace("u:", "ü").replace("U:", "Ü")
    if not transcriptions.is_pinyin(syl):
        return x[0]
    return transcriptions.to_pinyin(syl)

cedict = cedict.assign(**{
    "py": cedict["py"].str.replace(pinyin_match, pinyin_replace_nums, regex=True),
    "def": cedict["def"].str.replace(pinyin_match, pinyin_replace_nums, regex=True),
})

# Save
cedict.to_csv("cedict_parsed.tsv", sep="\t", index=False)
