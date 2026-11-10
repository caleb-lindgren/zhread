#!/bin/bash
# Makes demo/reader.gif, the demo of zhread the README points to, by playing
# the actions below on a simulated screen (gozhread -demo;
# `gozhread -demo help` lists the actions). Rerun it after changing zhread, or
# the actions, to make the GIF again. Needs Go and a font with Chinese in it,
# found with fc-match unless FONT names one. Cards the demo makes go to a
# scratch file, not cards.tsv.
#
# The parameters, each overridable from the environment:
#
#   FILE    the text read, relative to the repository root
#   START   text the cursor starts on
#   STEPS   how many words → and w move over from there
#   WORD    the word carded, searched for after the scanning; Enter
#           cards the word the cursor is on, so it must be one word as
#           zhread splits the text
#   SENSE   which of its Chinese senses the card takes instead of the first,
#           2 or more: the actions untick the first and tick this one
#   OUT     the GIF written
#   CARDS   if set, a new file to save the demo's cards to, for
#           scripts/cards_demo.py to show
#
#     FILE=book.txt START=第一句 WORD=詞語 SENSE=3 scripts/zhread_demo.sh

set -euo pipefail

FILE=${FILE:-demo/guxiang.txt}
START=${START:-我冒了嚴寒}
STEPS=${STEPS:-5}
WORD=${WORD:-禁不住}
SENSE=${SENSE:-2}
OUT=${OUT:-demo/reader.gif}
CARDS=${CARDS:-}
FONT=${FONT:-}

# rep C N is C N times, as a keys action takes a character, its * being
# the key of that name
rep() { local i; for ((i = 0; i < $2; i++)); do printf %s "$1"; done; }

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

go run ./cmd/zhread -demo - -gif "$OUT" ${CARDS:+-cards "$CARDS"} <<EOF
size 80 24
font 20 $FONT
open $FILE
find $START
delay 1200
caption none

# Read along a word at a time; the definitions follow the cursor.
delay 700
caption → moves by word
keys <Right>*$STEPS
pause 800

# Find the word, its next place after them.
delay 600
caption / searches
keys /$WORD
pause 800
keys <Enter>
pause 2500

# Card it, with the sense this context uses rather than the first.
delay 700
caption Enter makes a card of the word under the cursor
keys <Enter>
pause 1500
caption Space unticks the first sense...
keys <Space>
caption ...↓ goes to the one meant...
keys <Down>*$(( SENSE - 1 ))
caption ...and Space ticks it
keys <Space>
pause 1500
caption Enter saves the card
keys <Enter>
pause 2500

# Make it again with the vi keys.
forget
find $START
caption The same with vi keys: w moves by word
keys $(rep w $STEPS)
pause 800
delay 600
caption / searches
keys /$WORD
pause 800
keys <Enter>
pause 2500
delay 700
caption Enter makes the card
keys <Enter>
pause 1500
caption Space unticks the first sense...
keys <Space>
caption ...j goes to the one meant...
keys $(rep j $(( SENSE - 1 )))
caption ...and Space ticks it
keys <Space>
pause 1500
caption Enter saves the card
keys <Enter>
pause 2500
EOF
echo "wrote $OUT"
