# The dictionary directory

zhread reads the files here. Those built from downloads are not in the
repository: scripts/fetch_dict.sh (`make dict`) downloads each source and
runs its parser, and `--csld` and `--wikt` add the optional extras. The
rest are in the repository: jieba's and OpenCC's tables, vendored with
their licenses, and the corrections and additions below, written by hand.

| File                                  | Needed   | Built by             |
| ------------------------------------- | -------- | -------------------- |
| `cedict_parsed.tsv`                   | yes      | fetch_dict.sh        |
| `cedict_additions.tsv`                | yes      | by hand              |
| `MoEDICT/MoEDict-05-Simp03-cards.txt` | yes      | fetch_dict.sh        |
| `ccdict_additions.tsv`                | yes      | by hand              |
| `jieba/dict.txt`                      | yes      | vendored             |
| `opencc/*.txt`                        | yes      | vendored             |
| `fallback_exclude.tsv`                | no       | by hand              |
| `csld.tsv`, `csld_pron.tsv`           | no       | fetch_dict.sh --csld |
| `csld_fixes.tsv`                      | for csld | by hand              |
| `wikt.tsv`, `wikt_pron.tsv`           | no       | fetch_dict.sh --wikt |

The dates and counts below are those of the builds the parsers were
written and checked against; a fresh download will differ a little.

# CC-CEDICT

- Downloaded from
  <https://www.mdbg.net/chinese/export/cedict/cedict_1_0_ts_utf-8_mdbg.txt.gz>
  on 31 Oct 2026. The file's own date is 2026-10-31T01:52:43Z, with
  125,330 entries; the copy it replaced was dated 2023-07-31, with
  121,673.
- Parsed using parse_cedict.py in this directory to create cedict_parsed.tsv:
  `python parse_cedict.py cedict_1_0_ts_utf-8_mdbg.txt`, in a venv with pandas
  and dragonmapper. fetch_dict.sh does this. It skips the file's first 30
  lines, its comment header; fetch_dict.sh stops if a new download's header has
  another length. CC-CEDICT writes ü as u: (lu:4), which the script turns into
  ü before converting the tone number; until 4 Nov 2026 it did not, and 1,254
  entries kept lu:4.
- Additions, written by hand, in cedict_additions.tsv.

# MoEDICT - Pleco version (MoEDict-05-Simp03-cards.txt)

- The 教育部重編國語辭典修訂本 (Taiwan Ministry of Education Revised
  Chinese Dictionary), © 教育部, licensed CC BY-ND 3.0 TW. The Ministry
  reads the no-derivatives term as applying to the text itself, not to
  converting its format (see <https://github.com/g0v/moedict-data>).
- The Pleco flashcard export by alexhk90, with simplified headwords
  (MoEDict-05-Simp03), from
  <https://www.dropbox.com/s/avy920um3wuijpl/MoEDict-05-Simp03-cards.txt.7z>;
  see <https://github.com/alexhk90/Pleco-User-Dictionaries/tree/master/MoEDict>
  for how it is made. The copy extracted on 10 Nov 2026 is the same file,
  byte for byte, as the one downloaded in 2023.
- fix_moedict.py corrects 13 lines of it: the readings of six words carry a
  label for a variant reading, as " (語音) xiě" for 血, and 般 has the
  simplified headword 举. fetch_dict.sh applies it after extracting.
- Additions, written by hand, in ccdict_additions.tsv.

# jieba word frequencies (jieba/dict.txt)

- Vendored from the jieba 0.42.1 release on PyPI on 19 Sep 2026. The same
  file is at <https://github.com/fxsjy/jieba/blob/master/jieba/dict.txt>.
  MIT licensed; the license is in jieba/LICENSE.
- Format: one line per word, "word count POS", space separated. 349,046
  lines and 349,045 distinct words (B超 is listed twice, with the same
  count both times), counts summing to 60,101,967 and none below 2.
- The only per-word frequency data here, and the only reason it is here: a
  per-character frequency cannot choose between two segmentations of one
  run, since both readings of 交流感 are made of 交, 流 and 感.
- Read only by the viterbi segmenter. The headwords are simplified,
  but no conversion table is needed: a traditional candidate is looked up
  through the simplified headword of whatever entry matched it.
- Coverage of the definition sources, keyed on the simplified form:
  71.6% of CC-CEDICT headwords, 82.1% of the two-character ones, 56.2% of
  MoEDict's. The misses are long and rare entries; ambiguity is mostly
  among short common words. A word with no count is scored as one
  occurrence.

# Cross-strait dictionary (csld_pron.tsv)

- Derived from 兩岸詞典.csv of <https://github.com/g0v/moedict-data-csld>
  (中華語文知識庫), downloaded 30 Oct 2026, by parse_csld.py in this
  directory: `python3 parse_csld.py 兩岸詞典.csv > csld_pron.tsv`. The
  29 MB source is not kept. fetch_dict.sh --csld builds it.
- The content is © 中華文化總會, licensed CC BY-NC-ND 4.0 to the MoEDict
  project; the CSV conversion is CC0. No-derivatives forbids sharing what
  is built from it, so it is built locally, for personal study only, and
  is not in this repository.
- Columns: trad, simp, tw, cn, pinyin one space between syllables. The
  source writes each reading run together (qìyè) with ɡ and ɑ for g and a;
  the syllable count comes from its Zhuyin columns, and the split from the
  syllables CC-CEDICT uses, plus yái (崖 in Taiwan) and erhua spellings.
- Of 101,686 rows, 8,007 give a Mainland reading, which the source fills
  in only where it differs. 68 of those have no Taiwan reading to pair it
  with, and 5 cannot be split into syllables because of errors in the
  source (術家 has the Mainland reading "ā"). Of the 7,934 left, 1,491 are
  dropped as differing only in notation: the Taiwan column writes 一/不
  tone sandhi (一人 yìrén against yīrén) and erhua as spoken (一點兒 yìdiǎr
  against yīdiǎnr). That leaves 6,443 rows over 6,349 traditional
  headwords.
- Typos in the source's pinyin are corrected from csld_fixes.tsv first,
  as they are for csld.tsv; see below.

# Cross-strait dictionary corrections (csld_fixes.tsv)

- The source gives each reading in Zhuyin as well as pinyin. check_csld.py
  in this directory converts the Zhuyin and lists the readings where the
  two disagree: `python3 check_csld.py 兩岸詞典.csv`. Of 109,621 readings,
  109,527 were checked; the other 94 have Zhuyin it does not read
  (Taiwan's spoken erhua finals, as ㄏㄨㄜˇㄦ for 會兒, and look-alikes
  such as ｜ for ㄧ). 60 disagreed, each reviewed by hand into
  csld_fixes.tsv: columns trad, side (tw or cn), the source's pinyin, the
  reading to use, and a note.
- 52 are typos in the pinyin, corrected from the Zhuyin: 坑人 kēngrēn,
  奎 kúi, 呼牛呼馬 hūniú-hūm, ɑ inside a word (世代 shìdɑì), tone marks
  missing (削壁 xuebì) or on the wrong vowel (互惠 hùhùi). 朊 is ruǎn,
  as in CC-CEDICT: the Zhuyin lacks the ㄖ too.
- 8 keep the pinyin, the source column repeated: the Zhuyin is wrong (千里姻緣一線牽
  has ㄩˊ for 緣, 隆裕太后 the Zhuyin of 皇后) or writes erhua as a syllable of its own
  (勁兒 ㄐㄧㄣˋ ㄦ).
- parse_csld.py and parse_csld_defs.py apply a fix only where the source
  still has the pinyin it was made against, so after a new download,
  check_csld.py lists any typo whose fix no longer applies along with any
  new ones.

# Fallback exclusions (fallback_exclude.tsv)

- Words for which zhread does not take MoEDict's reading for Taiwan's and
  CC-CEDICT's for the Mainland's (see "Taiwan and Mainland readings" in the
  top-level [README](../README.md)), because the two entries are two senses
  read differently: 下咽 xià yàn "to swallow" against xià yān "hypopharynx".
- Written by hand on 4 Nov 2026, from a review of all 748 words the
  fallback then applied to, with each pair's MoEDict and CC-CEDICT
  definitions side by side: 28 are listed. Columns: trad, MoEDict's
  reading, CC-CEDICT's, a note naming the two senses. An entry holds
  only while both dictionaries still give the readings it lists.

# Cross-strait dictionary definitions (csld.tsv)

- From the same 兩岸詞典.csv as csld_pron.tsv (downloaded again 31 Oct
  2026 from
  <https://raw.githubusercontent.com/g0v/moedict-data-csld/master/兩岸詞典.csv>,
  29,199,151 bytes), by parse_csld_defs.py in this directory:
  `python3 parse_csld_defs.py 兩岸詞典.csv > csld.tsv`. Same license:
  CC BY-NC-ND 4.0, personal study only.
- zhread's main source of Chinese definitions; MoEDict
  is their fallback.
- Columns: trad, simp, tw, cn, def, unquoted. Every one of the 101,686 rows is
  kept. The readings are split into syllables as in csld_pron.tsv, or left as
  written where they cannot be; cn is empty where the source gives no separate
  Mainland reading. def is the row's definition cells joined by ¶, with the
  source's ～ for the headword replaced by the headword, and its examples, which
  follow [例] separated by bars, rewritten in MoEDict's form, 如：「…」、「…」。. The
  source ends 90,044 of its 92,326 example lists with 。, which goes after the
  last closing bracket. Quotations, those ending with their source in 《》 or 〈〉,
  are cells of their own. The source's HTML (tables, superscripts, subscripts,
  in 80 rows) is turned into text, a table row to a cell. See the script's
  docstring for the layout.

# Wiktionary (wikt.tsv)

- From wiktextract's extract of English Wiktionary's Chinese entries,
  <https://kaikki.org/dictionary/Chinese/kaikki.org-dictionary-Chinese.jsonl>
  (1,214,692,243 bytes, Last-Modified 30 Oct 2026), streamed through
  parse_wikt.py in this directory on 1 Nov 2026; the command is in its
  docstring, and fetch_dict.sh --wikt runs it. The dump is not kept.
  Wiktionary's text is CC BY-SA 4.0.
- zhread's fallback source of English definitions,
  after CC-CEDICT.
- 161,670 rows, one per word, part of speech and etymology, over 148,195
  traditional headwords, with 214,644 senses. Kept: Mandarin senses with
  a gloss, at most three examples and one quotation each (24,838 and
  9,729 in all). Dropped: simplified-spelling redirects (used for the
  simplified column), senses tagged only for other varieties
  (Cantonese, Hokkien and the rest) or labeled only with one, as
  "(Shanghainese) currently" is with no tag, examples in other
  varieties (tagged with their romanization, or romanized with tone
  digits, as Jyutping's hoeng¹), and records left with no sense. The
  ～ in labels such as "(～市) Kyoto" is replaced by the headword.
- Readings: wiktextract gives a record the readings of the page's other
  etymologies too, so the reading is taken from the bold syllables of
  the record's first example, and only failing that from the first
  standard reading listed (which gets 打 "only used in 蘇打" wrong: dǎ for
  dá). Readings tagged "phonetic", which spell out tone sandhi (理髮 lífǎ
  for lǐfǎ), are skipped, and so is the "[Phonetic: ..." that some
  readings carry after them. An erhua 兒 is split as part of the syllable
  before it (這兒 zhèr). 4,689 rows note a differing Taiwan reading.

# Wiktionary reading pairs (wikt_pron.tsv)

- From wikt.tsv and csld_pron.tsv, by parse_wikt_pron.py in this
  directory: `python3 parse_wikt_pron.py > wikt_pron.tsv`. Same columns
  and spelling as csld_pron.tsv, and read after it.
- 2,269 pairs: each wikt.tsv row with a Taiwan reading, unless
  csld_pron.tsv has the word (2,003 of Wiktionary's pairs match a row
  there exactly and 89 do not, mostly because the cross-strait table
  writes 一/不 sandhi on the Taiwan side), the pair differs only in
  notation, or a reading cannot be split into syllables (13).

# OpenCC (opencc/)

- The dictionary tables of OpenCC 1.4.2, from the ver.1.4.2 tag of
  <https://github.com/BYVoid/OpenCC> (data/dictionary/), downloaded 30 Oct
  2026, for zhread's simplified/traditional conversion of card contexts.
  Apache-2.0; the license is in opencc/LICENSE.
- STPhrases_GeneratedFromRegionalPhrases.txt is not in the repository:
  OpenCC generates it at build time. It was dumped from the .ocd2 file of
  the opencc 1.4.2 Python package.
- Only the tables of the s2tw and tw2s configs are here: 1.25 MB in all,
  1.0 MB of it STPhrases.txt. internal/opencc chains them as those configs
  do, and its s2tw output matched the package's on all 12,034 sentences of
  the comparison below. tw2s differed on 6, all from TSCharactersExt, which
  OpenCC marks as possibly producing characters fonts lack, and which is
  left out.
- Chosen over converting word by word through CC-CEDICT and MoEDict after a
  comparison on 476k characters of natively traditional text (937 random
  zh.wikipedia articles and 46 ROC presidential speeches), simplified character
  by character with Unicode's kSimplifiedVariant mappings and converted back.
  Counting errors over the characters where traditional and simplified differ
  or the method changed something (about 103k and 55k), s2tw got 1.76% and
  2.24% wrong, the dictionary method 2.24% and 2.74% at best. The dictionary
  method's errors are senses it cannot tell apart (制/製, 只/隻, 系/係); about 600 of
  each method's errors on the Wikipedia text are 臺 where the text writes 台.
