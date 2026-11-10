#!/bin/bash
# Downloads the dictionaries zhread reads and builds its tables in dict/.
# `make dict` runs it in the repository's venv; dict/README.md describes each
# source, its licence and what the parsers do to it.
#
#     scripts/fetch_dict.sh [--csld] [--wikt]
#
# Always built, since zhread needs them:
#
#   cedict_parsed.tsv               from CC-CEDICT (4 MB download)
#   MoEDICT/MoEDict-05-Simp03-cards.txt
#                                   from the Pleco export of the MoE
#                                   dictionary (12 MB), with the corrections
#                                   in fix_moedict.py
#
# Optional extras, which zhread uses when they are there:
#
#   --csld  csld.tsv, csld_pron.tsv from the cross-strait dictionary
#           (兩岸詞典, 29 MB): Chinese definitions in the dictionary's own
#           layout, and its table of Taiwan and Mainland readings. Licensed
#           CC BY-NC-ND 4.0: build it for your own use, and do not share
#           what is built.
#   --wikt  wikt.tsv, wikt_pron.tsv from English Wiktionary, through
#           kaikki.org's extract (1.2 GB, streamed, not kept): English
#           definitions where CC-CEDICT has none, and more reading pairs.
#           Takes several minutes. With --csld too, the csld tables are
#           built first, as wikt_pron.tsv leaves out the words they have.
#
# PYTHON names the interpreter, which needs requirements.txt installed;
# the default is the repository's .venv. Each table is written to a
# temporary file and moved into place only when complete, so a failed run
# leaves the previous tables as they were.

set -euo pipefail

CEDICT_URL=https://www.mdbg.net/chinese/export/cedict/cedict_1_0_ts_utf-8_mdbg.txt.gz
MOEDICT_URL='https://www.dropbox.com/s/avy920um3wuijpl/MoEDict-05-Simp03-cards.txt.7z?dl=1'
CSLD_URL=https://raw.githubusercontent.com/g0v/moedict-data-csld/master/%E5%85%A9%E5%B2%B8%E8%A9%9E%E5%85%B8.csv
WIKT_URL=https://kaikki.org/dictionary/Chinese/kaikki.org-dictionary-Chinese.jsonl

root=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/..
PYTHON=${PYTHON:-$root/.venv/bin/python}
# Made absolute, as the parsers are run from other directories, but not
# with readlink -f, which would resolve a venv's python to the system one.
if [[ $PYTHON == */* ]]; then
    PYTHON=$(cd "$(dirname "$PYTHON")" && pwd)/$(basename "$PYTHON")
fi

csld=0
wikt=0
for arg; do
    case $arg in
    --csld) csld=1 ;;
    --wikt) wikt=1 ;;
    *)
        echo "usage: $0 [--csld] [--wikt]" >&2
        exit 2
        ;;
    esac
done

if ! "$PYTHON" -c 'import pandas, dragonmapper, py7zr' 2>/dev/null; then
    echo "$PYTHON lacks requirements.txt's packages; run 'make dict'," >&2
    echo "or set PYTHON to an interpreter that has them" >&2
    exit 1
fi

cd "$root/dict"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

step() { printf '\n== %s\n' "$*"; }

step "CC-CEDICT"
curl -fL --progress-bar "$CEDICT_URL" | gunzip >"$tmp/cedict.txt"
# parse_cedict.py skips the first 30 lines as the file's comment header.
# A header of another length would make it drop or misread entries.
comments=$(awk '!/^#/ { print NR - 1; exit }' "$tmp/cedict.txt")
if [[ $comments != 30 ]]; then
    echo "CC-CEDICT's header is $comments lines, not the 30" \
        "parse_cedict.py skips; update its skiprows" >&2
    exit 1
fi
grep -m1 '^#! date=' "$tmp/cedict.txt"
(cd "$tmp" && "$PYTHON" "$root/dict/parse_cedict.py" cedict.txt)
mv "$tmp/cedict.txt" cedict_1_0_ts_utf-8_mdbg.txt
mv "$tmp/cedict_parsed.tsv" cedict_parsed.tsv
echo "cedict_parsed.tsv: $(($(wc -l <cedict_parsed.tsv) - 1)) entries"

step "MoEDict (Pleco export)"
curl -fL --progress-bar -o "$tmp/moe.7z" "$MOEDICT_URL"
"$PYTHON" -c 'import py7zr, sys
py7zr.SevenZipFile(sys.argv[1]).extractall(sys.argv[2])' "$tmp/moe.7z" "$tmp"
"$PYTHON" fix_moedict.py "$tmp/MoEDict-05-Simp03-cards.txt"
mkdir -p MoEDICT
mv "$tmp/MoEDict-05-Simp03-cards.txt" MoEDICT/
echo "MoEDICT/MoEDict-05-Simp03-cards.txt:" \
    "$(wc -l <MoEDICT/MoEDict-05-Simp03-cards.txt) lines"

if ((csld)); then
    step "兩岸詞典 (CC BY-NC-ND 4.0: for your own use only)"
    curl -fL --progress-bar -o "$tmp/csld.csv" "$CSLD_URL"
    "$PYTHON" parse_csld.py "$tmp/csld.csv" >"$tmp/csld_pron.tsv"
    "$PYTHON" parse_csld_defs.py "$tmp/csld.csv" >"$tmp/csld.tsv"
    mv "$tmp/csld_pron.tsv" "$tmp/csld.tsv" .
    echo "csld.tsv: $(wc -l <csld.tsv) lines;" \
        "csld_pron.tsv: $(wc -l <csld_pron.tsv) lines"
fi

if ((wikt)); then
    step "Wiktionary (streamed; several minutes)"
    curl -fL --progress-bar "$WIKT_URL" |
        "$PYTHON" parse_wikt.py >"$tmp/wikt.tsv"
    mv "$tmp/wikt.tsv" .
    "$PYTHON" parse_wikt_pron.py >"$tmp/wikt_pron.tsv"
    mv "$tmp/wikt_pron.tsv" .
    echo "wikt.tsv: $(wc -l <wikt.tsv) lines;" \
        "wikt_pron.tsv: $(wc -l <wikt_pron.tsv) lines"
fi

step "done"
