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
pinyin_match = r"\b[A-Za-z][a-z]*[1-5]\b"
pinyin_replace_nums = lambda x: transcriptions.to_pinyin(x[0]) if transcriptions.is_pinyin(x[0]) else x[0]

cedict = cedict.assign(**{
    "py": cedict["py"].str.replace(pinyin_match, pinyin_replace_nums, regex=True),
    "def": cedict["def"].str.replace(pinyin_match, pinyin_replace_nums, regex=True),
})

# Save
cedict.to_csv("cedict_parsed.tsv", sep="\t", index=False)
