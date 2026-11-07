#!/bin/bash
# Makes zhread.gif, the demo of zhread the README points to, by playing the
# actions below on a simulated screen (gozhread -demo; `gozhread -demo help`
# lists the actions). Rerun it after changing zhread, or the actions, to
# make the GIF again. Needs Go and a font with Chinese in it, found with
# fc-match unless FONT names one. Cards the demo makes go to a scratch file,
# not cards.tsv.
#
# The parameters, each overridable from the environment:
#
#   FILE    the text read, from the repository root
#   START   text the cursor starts on
#   STEPS   how many words Ctrl-Right moves over from there
#   WORD    the word carded, searched for after the scanning
#   SENSE   which of its Chinese senses the card takes instead of the first
#   OUT     the GIF written
#
#     WORD=萬物 SENSE=1 scripts/zhread_demo.sh

set -euo pipefail
export LC_ALL=C.UTF-8 # so that ${#WORD} counts characters

FILE=${FILE:-texts/daodejing.txt}
START=${START:-道可道}
STEPS=${STEPS:-8}
WORD=${WORD:-天下}
SENSE=${SENSE:-2}
OUT=${OUT:-zhread.gif}
FONT=${FONT:-}

root=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/..
cd "$root"

go run ./cmd/zhread -demo - -gif "$OUT" <<EOF
size 80 24
font 20 $FONT
open $FILE
find $START
delay 1200
caption none

# Read along a word at a time; the definitions follow the cursor.
delay 700
caption Ctrl+→ moves by word
keys <C-Right>*$STEPS
pause 800

# Find the word, its next place after them, and select it.
delay 250
caption / searches
keys /$WORD
delay 700
keys <Enter>
caption Shift+→ selects
keys <S-Right>*$(( ${#WORD} - 1 ))
pause 600

# Card it, with the sense this context uses rather than the first.
caption Enter makes a card of the selection
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
EOF
echo "wrote $OUT"
