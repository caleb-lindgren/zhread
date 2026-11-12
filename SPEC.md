# zhread: the details

How zhread works, in full: what it shows and why, every key, how cards are
made and kept, and how the dictionaries are combined. [README.md](README.md)
is the place to start; this is the reference behind it.

## Dictionaries

The dictionaries are not in the repository, for their size and their
licenses; `make dict` downloads them and builds dict/'s tables:

| Source              | Gives                              | License         | Built by                |
| ------------------- | ---------------------------------- | --------------- | ----------------------- |
| CC-CEDICT           | English definitions                | CC BY-SA 4.0    | `make dict`             |
| MoEDict (教育部重編國語辭典) | Chinese definitions                | CC BY-ND 3.0 TW | `make dict`             |
| 兩岸詞典 (cross-strait) | Chinese definitions, readings      | CC BY-NC-ND 4.0 | `make dict ARGS=--csld` |
| Wiktionary          | English definitions, readings      | CC BY-SA 4.0    | `make dict ARGS=--wikt` |
| jieba's dict.txt    | word frequencies, for segmentation | MIT             | in the repository       |
| OpenCC's tables     | simplified/traditional conversion  | Apache-2.0      | in the repository       |

`ARGS="--csld --wikt"` builds both extras. 兩岸詞典's license forbids sharing
adaptations, so its tables are built for your own use only. The Wiktionary
build streams a 1.2 GB extract and takes several minutes. zhread runs without
either extra, with fewer definitions and fewer Taiwan readings.
[dict/README.md](dict/README.md) describes each source and what its parser
does, and `dict/*_additions.tsv` and `dict/fallback_exclude.tsv` are
corrections and additions written by hand.

## Reading

The words of the text come from the segmenter (see "Segmentation" below), run
in the background a screenful at a time, and the definitions of the word under
the cursor (or of the selection) are shown below the text, each starting on a
line with the word and its reading, and every line of it indented to where its
first line starts. They are Chinese only until `e` adds the English, except for
a word with no Chinese definition, whose English is shown at once.

Each language has a main dictionary and a fallback. Chinese comes from
兩岸詞典 (the cross-strait dictionary), whose labels 〈書〉 (literary) and
〈口〉 (colloquial) are kept, and whose marks for a word or sense used only
on the Mainland (★) or in Taiwan (▲) are spelled out as 〈大陸〉 and
〈臺灣〉; MoEDict defines the words 兩岸詞典 lacks, and `c` adds its
definitions of the rest. English comes from CC-CEDICT; Wiktionary defines
the words CC-CEDICT lacks or only points elsewhere for ("variant of …",
"see …"), and `E` adds its definitions of the rest. A fallback's
definition shown beside the main one is labeled with its name, under the
word. 兩岸詞典 and Wiktionary are optional extras (see "Dictionaries"
below); without them, MoEDict is the main Chinese dictionary and CC-CEDICT
the only English one. See [dict/README.md](dict/README.md) for the files.

A word is shown in traditional, with the simplified form after it in brackets
when it differs and the characters the two share as ～: 企業（～业）. An entry that is
only a variant of another of the word's is left out; when the text has the
variant form, as 煖 for 暖, the standard form's entries are shown with the
variant after the word, as "暖 (煖)". Which form the text has is judged from the
words around it: the script that more of them match only one of.

Each sense gets a line, with its examples and synonym lists after it on the
same line, in colors of their own. A part of speech starts the first sense
under it, and the rest line up after it; a sense with sub-senses, like
MoEDict's ① 結構助詞： over ⑴ to ⑷, has a line of its own, with the sub-senses
indented under it. Quotations from books are hidden until `x`, and then shown
under the sense. A sense already on a card ends with "(carded)", after a space
in English, matched against the definitions saved in the card file; a carded
word none of whose senses shown matches, as one carded from the other
dictionary, has "(carded)" under the word instead. The definitions pane is as
tall as what it shows, up to half the screen, so short entries leave the text
more room; it is measured again on every redraw, a resize included. A longer
entry scrolls with `J` and `K`, and the rule above it notes how many rows are
out of view (↑2 ↓5), and before that which of `x`, `c`, `e` and `E` would
change what the pane shows for the word: `x` when a definition shown has
quotations it hides, `c` when MoEDict has a definition beside 兩岸詞典's, `e` when
there are English definitions after Chinese ones, `E` when Wiktionary has one
beside CC-CEDICT's while English is shown. `gh` or F1 shows the keys there
instead.

Readings are Mainland (see "Taiwan and Mainland readings" below); `t` adds the
Taiwan reading where it differs, as "qǐ yè (TW qì yè)", and a card made while
it is shown carries both. `-pron tw` reverses the two. Words already carded are
colored. The cursor is white, and the rest of the word under it blue, except
in visual mode, where only the selection is colored.

The text scrolls only as far as keeps the cursor in view, as in vi: a move
within the screen moves only the cursor, `^D ^U ^F ^B`, PgUp and PgDn scroll
the text as far as they move the cursor, and the wheel scrolls the text under
the cursor, which moves only to stay in view. A jump to more than half a screen
out of view, as a search's, the bookmark's or `G`'s, puts the cursor's line a
quarter of the way down the screen, with text on both sides of it. With `-pin`,
the cursor's line stays a quarter of the way down whatever the cursor does,
except near the start of the file, so the text does not shift as the
definitions pane changes height, and the wheel moves the cursor a row a notch.
A pane moved into with Tab always scrolls as without `-pin`.

The colors are those of Zenburn, Jani Nurminen's color scheme for Vim
(<https://github.com/jnurmine/Zenburn>), as the vis editor's port of it
gives them (lua/themes/zenburn.lua in <https://github.com/martanne/vis>).
They are all xterm 256-color palette entries, so st shows them exactly
without true color. The full list of keys:

| keys                | action                                               |
| ------------------- | ---------------------------------------------------- |
| `h j k l`           | character left, line down, line up, right            |
| `w b`, M-f M-b      | next word, previous word                             |
| `0 $`, `g0 g$`      | start, end of the line; of the screen row            |
| `gj gk`             | screen row down, up                                  |
| ← →, ^← ^→, M-← M-→ | word back, forward; character left, right            |
| ↑ ↓, ^↑ ^↓, M-↑ M-↓ | screen row up, down; paragraph back, forward         |
| Home End            | start, end of the screen row; with Ctrl, of the text |
| PgUp PgDn           | page up, down                                        |
| Shift-              | with any of the four above: select as it moves       |
| `gg G`              | first line, last line (or line N with a count)       |
| `^D ^U ^F ^B`       | half page down, up; page down, up                    |
| `/ ?`, `n N`        | search forward, back (a Go regexp); repeat           |
| `* #`               | search for the word under the cursor                 |
| `v V`               | select by character, by line                         |
| Enter               | make a card for the word, or for the selection       |
| `u`                 | undo this session's last card or update (asks `y`)   |
| `:>cmd`             | in `v` or `V`: pipe the selection to `cmd`           |
| `m`, `'`            | bookmark this line, go to the bookmark               |
| `e`                 | hide or show English definitions (shown at start)    |
| `x`                 | show or hide quotations                              |
| `c`, `E`            | show or hide MoEDict, Wiktionary as well             |
| `J K`               | scroll the definitions down, up                      |
| M-PgDn M-PgUp       | the same, for those who do not use vi's keys         |
| Tab, Shift-Tab      | move into the definitions; back up a level           |
| `t`                 | show or hide the other side's reading                |
| `s`                 | single characters, not words; again for words        |
| `gh`, F1            | show the keys                                        |
| Esc                 | hide them; end a selection; back to the book         |
| `q`                 | quit                                                 |
| click, wheel        | move the cursor there; scroll the text               |
| double-click        | make a card for the word, as Enter                   |
| wheel on the pane   | scroll the definitions, as J and K                   |
| drag                | select from where the button was pressed             |
| click a key         | on the rule, or 詞/字 and a hint on the status line    |

Tab moves the cursor into the definitions pane, which becomes the text: every
key above works on it as on the book, making cards and piping included, and a
pane below it defines the word under the cursor there. The cursor goes only
over the definitions, passing over the words, readings, sense numbers (1., (1),
①) and notes on them, as it passes over the number a numbered paragraph of the
book starts with. `j` and `k` keep its column on the screen, so they go
straight down a pane whose first line starts further left than the rest. The
pane below keeps half the screen however little it shows, so nothing above it
moves as the cursor goes from word to word. Tab again goes a level deeper. The
levels stack down the screen, each as tall as it was as a pane, and once they
no longer fit, the oldest are cut off at the top. Each level above marks in
blue the word it looked up. Shift-Tab goes back up a level, closing the pane it
leaves, and Esc goes back to the book from however deep. A click in the pane
moves into it, and one in a level above goes back up to it. A card made in a
pane takes the sense it is in as its context, and gets the book's line and the
words looked up as its source, as `/home/me/book.txt:12 › 讨论 › 交換`.
Bookmarks are for the book only.

The arrows, Home, End, Page Up and Page Down move as in an editor that wraps
its lines: ← and → go on past the end of a line to the next, ↑ ↓ Home and End
work on screen rows, and Ctrl-↑ and Ctrl-↓, as vi's `{ }`, go to the start of a
paragraph, a line after a blank one (in a pane, any line). With Shift they
select from where the cursor was. A selection begun so, or by dragging the
mouse, ends at the next move without Shift, and at a click, as an editor's
does; one begun with `v` or `V` stays until Esc, as in vi. Dragging needs the
terminal to report the mouse moving with the button held (xterm's mode 1002),
as st and xterm do; in both, Shift with the mouse still selects the terminal's
own text, for copying.

M- is Alt, Option on a Mac, where Ctrl-arrows belong to Mission Control and the
terminal keeps Cmd for itself: Option-← and Option-→ move by character and
Option-↑ and Option-↓ by paragraph, as Ctrl does, with Shift to select. The
plain arrows ← and → move by word, the unit read in, so Ctrl or Option is for
the rarer move by character. Terminal.app sends them only with "Use Option as
Meta key" ticked (Settings > Profiles > Keyboard). iTerm2 sends Option-← and
Option-→ as Esc b and Esc f unless its profile maps them otherwise, and zhread
takes Esc b and Esc f, and Esc B and Esc F with Shift, as word left and right,
as the plain arrows are; Option-arrows then move by word in iTerm2, so for
characters map them to send Alt-arrows.

The status line's right end is a menu button. A click on it opens a menu of
the actions with nowhere else to click: undo the last card (`u`), bookmark
this line (`m`), go to the bookmark (`'`), show the keyboard shortcuts (`gh`,
F1) and quit (`q`), each shown with its key. In the menu, ↑ ↓ `j k` move,
Enter or Space does the item, and Esc or a click off the menu closes it; any
other key closes it and does what it does outside, so an item's own key works
there too. After a card is saved, the message saying so is followed by an undo
button, a click on which is `u`, and the question `u` asks has clickable
answers, shown on the whole status line.

Everything a click does something on, other than the text, the definitions and
the card maker's lines, is a button with a green background: the menu and undo
buttons, 詞 or 字, the keys on the definitions' rule, hints such as "Enter:
make a card", and in the card maker its buttons and toggles.

`-controls` chooses the ways of working zhread, any of `arrows`, `vi` and
`mouse`, comma-separated; all three by default. `arrows` are the arrows, Home,
End, Page Up, Page Down and F1, with their modifiers, and Esc b and Esc f.
`vi` are vi's motions and counts, `v V`, `J K`, `g` and the commands it
begins, and in the card maker `j k`, `[ ] { }` and the editor's normal mode:
without `vi`, `i` starts the editor typing and Esc leaves it. Keys in neither,
such as Enter, Space, Esc, `/`, `n` and the letters that toggle what is shown,
always work, as do the arrows and vi's keys in the search prompt and the
editor, where they edit text. Without `mouse`, zhread leaves the mouse to the
terminal, which selects its text as usual, and shows no buttons. The keys `gh`
and F1 list, and the menu's, are only those of the ways left.

Counts work as in vi (`5j`, `3w`). `:>cmd` starts `cmd` with sh, the
selected text on its standard input, and goes straight back to reading:
it is meant for commands that open a window of their own, such as
`:>st -e less` or `:>xclip -sel c`. The command runs in a session
of its own, so it outlives zhread, and its output is discarded; if it
fails, the end of what it wrote to stderr is shown in the status line. A
search in visual mode moves the end of the selection, as in vi.

The bookmark is one per file: setting it replaces the old one, and opening the
file goes to it. Bookmarks are kept in `$XDG_STATE_HOME/zhread/bookmarks`
(`~/.local/state` by default), with the start of the marked line, so the mark
finds its line again after the file is edited above it.

## Making a card

Press Enter on the word, or select it with `v` and press Enter. A selection
that no dictionary has as one word, such as a phrase, is split by the
segmenter into the words in it that have definitions, and each gets a card of
its own, in turn, each with the context around it; `n` skips one and Esc
cancels the rest.

When the words can be divided more than one way, a screen asks how first:
牟取暴利 is one word to CC-CEDICT, and also 牟取 and 暴利. It offers the
word, or for a phrase the segmenter's words, then the other ways to divide it
wholly into words of two characters or more (words of one character are left
out, or every 散发 would offer 散 and 发), and for a phrase what the forward and
backward maximum-matching segmenters make of it. `j`/`k` and Enter, or a
digit, choose, as does a click on one; Esc cancels.

The preview shows:

- **The context**, in both scripts when the word differs between them, else in
  the text's own, the cards then being made only in that script. It starts as
  the sentence holding the word: the text between sentence-ending marks (。！？…),
  with any closing quotes or brackets after them. A sentence under 12
  characters (「好。」) gets the sentence before it, or after it at the start of a
  paragraph; one over 60 is cut to the clauses nearest the word, at ，；：、. `[`
  and `]` move the left edge a clause left or right, `{` and `}` the right
  edge. A context that stops short of its sentence's end leaves out the comma
  or other clause mark it stops at. Lines not separated by a blank line are one
  paragraph, so a sentence hard-wrapped across lines is found whole. A doubled
  form of the word, AABB or ABAB for a word AB (松松垮垮, 研究研究) and AA, A一A or A了A
  for a word A (看看, 看一看, 看了看), is marked and blanked whole rather than the word
  in its middle, though the card's word stays the one selected. A new card has
  this context alone; a card gets more only from later cards for the same word
  added to it (see "Cards already made" below).  `k` from the first definition
  goes up onto the context, in the list like the definitions. `i` starts vi's
  editor on it, in normal mode: `h l w b e 0 ^ $` move (a run of Chinese
  characters is one word, as in vi, so `dw` deletes to the next punctuation),
  `x X r s D C`, and `d` or `c` with a motion (`dw`, `cb`, `d$`, `dd`) change,
  `i a I A` insert until Esc, and `u` undoes. For those who do not use vi, the
  arrows, Home and End move, Backspace and Delete delete, and any other key
  types itself, starting insert mode before the cursor, so the editor can be
  typed in without knowing `i`. Esc in normal mode goes back to the list, as do
  `j` and `k`, which move on as well; Enter saves. The bottom line starts with
  the mode: `-- LIST --`, `-- NORMAL --` or `-- INSERT --`; after it come only
  the card's questions and warnings, and at its right, as on the reader's rule,
  which of `x` (a ticked definition has quotations), `c` and `E` would change
  the card. `e` never would: the card lists the English definitions whatever it
  says. The keys are listed instead by `gh` or F1, as in the reader, in a pane
  below the card that `J` and `K` scroll. Esc in the list hides the keys before
  it cancels. Editing the text's own script converts the other script's line
  again, unless that line has been edited itself. The edge keys start again
  from the text, undoing the edits.
- **The definitions** shown in the pane when the card was begun (so MoEDict's
  and Wiktionary's beside the main ones only after `c` or `E`), Chinese first,
  each sense an option of its own, with its examples, so a card carries the
  sense the context uses rather than all of 打's. The list scrolls to keep the
  cursor in view, and PgDn and PgUp move ten at a time. `j`/`k` move and Space
  ticks the ones that fit the context; the first Chinese and first English are
  ticked to start with. `c` and `E` show or hide MoEDict's and Wiktionary's
  definitions here too, keeping what is ticked. A ticked sense's quotations go
  on the card if `x` is showing them when it is saved, and are shown under it
  in the preview. The reading comes from the first ticked one.
- **Cards already made** for the same word, or for a word containing it or
  contained in it, in either script, with their context. When there are any,
  the first Enter does not save but asks again. For a card for the word itself,
  it offers the choice: a second Enter makes a new card, as for a sense the
  cards so far do not have, and `u` adds this context to the existing card
  instead. Cards for the word itself are numbered, and `u` (which works before
  that first Enter too) asks which by number when there are several. It opens
  that card in the preview in place of the new one, filled in as the card has
  it: its definitions ticked (MoEDict's, Wiktionary's or the quotations shown
  if it has them, and any no dictionary has any more kept as they are, labeled
  "on the card"), its reading, Taiwan's too if it has that, and its own
  context, then its other contexts in their order, then last the context `u`
  was pressed from, all ticked; a context the card has already is not added
  again. Everything works as for a new card: tick and untick definitions and
  contexts (an unticked context leaves the card), edit them with `i`, move
  their edges; `<` and `>` move the context under the cursor up and down the
  card's order. Enter saves it over the card's row of the card file, keeping
  its GUID, source and date, so the import updates the note in place. So each
  new card for the word that is added to its card this way adds one context to
  it, keeping those before, and Anki shows them all in turn (see "Into Anki"
  below). For such a word the senses ticked to start with on a new card are the
  ones already on its cards, so a new card for another sense is a matter of
  unticking those and ticking that one.

A click on a line of the card moves the cursor to it, out of the editor if it
was in one, a click on a `[ ]` box ticks or unticks it, as Space does, and a
double click on a context edits it, as `i` does; the button held or dragged
ticks it once. The bottom line has buttons for the mouse: `<start` `start>` and
`<end` `end>` move the context's edges as `[ ]` and `{ }` do (and Shift-←
Shift-→, and Alt or Ctrl with them, from the keyboard), save saves, and cancel
cancels, asking first when the card has been changed; a question or warning
takes their room, and its answers, as "y: yes", are clicked instead. Last,
where there is room, a menu button opens a menu of the actions that apply where
the cursor is and have no button: edit this context (`i`), move it up or down
(`< >`), edit the word's card instead (`u`), skip to the next word (`n`), and
show the keyboard shortcuts. In the reader, the keys the rule names (x c e E
t), 詞 or 字 on the status line (s), and its hints in visual mode are clicked as
those keys. Enter saves, Esc cancels. An Esc right after another, as from
insert mode out past the list, asks first when the card has been changed (a
context edited or its edges moved, or a tick changed), and only `y` then throws
the card away. The other script's context comes from OpenCC's s2tw or tw2s,
chosen by which changes the paragraph more (see
[dict/README.md](dict/README.md) for why OpenCC). When the word is the same in
both scripts, the traditional fields are left empty and only the simplified
cards are made; for a traditional text it is the simplified contexts that are
left empty, and only the traditional cards are made. The source is the text's
full path and the line, so a card can be traced back to its book from anywhere;
it is kept in the card file but not shown on the card. The reading is followed
by its zhuyin, "zǔ hé ㄗㄨˇ ㄏㄜˊ".

## The card file

Cards are appended to `$XDG_DATA_HOME/zhread/cards.tsv`, which is
`~/.local/share/zhread/cards.tsv` when `XDG_DATA_HOME` is unset (`-cards`
to change). It is the record of what has been carded and the file Anki
imports. Back it up, or keep it under version control: it is how the
record survives.

## Into Anki

Once, import anki/zhread.apkg (File > Import, or open it). It creates the
zhread note type: the thirteen fields, the four card types and the styling
from anki/. It holds one placeholder note, which has no cards and so never
comes up in study; Anki imports a note type only along with a note that
uses it, so a package without one would create nothing.

After that, import cards.tsv with File > Import whenever there are new cards.
Its first lines are Anki's import headers, naming the note type, the deck
(`main`, or `-deck`) and the GUID column, so the import dialog needs no
settings. The GUID is a hash of the word and its context, so importing the
whole file again updates the notes Anki already has and adds the new ones,
without duplicates and keeping review history. An update overwrites the note's
fields, so fix a card in cards.tsv rather than in Anki, or the next import
undoes the fix.

The exception is a field left empty in cards.tsv: the import keeps what
Anki has there rather than clearing it (seen 2026-11-09, when clearing the
mined contexts from cards.tsv left them in Anki). Before importing a
cards.tsv that empties fields Anki has filled, clear those fields in Anki,
which keeps review history, as it is held by the cards rather than the
fields:

1. In Browse, search `note:zhread` and select every row (Ctrl+A). Select
   them before the next step: the In list of the dialog offers the fields
   of the notes selected when it opens, and only All Fields and Tags when
   none are.
2. Notes > Find and Replace. Find `(?s).+`, Replace With empty, In the
   field to clear, with "Treat input as regular expression" and "Selected
   notes only" ticked. Not In All Fields, which would empty every field.
3. Repeat for each field to clear: for contexts, both Contexts and
   ContextsTrad.
4. Check that a search for `note:zhread Contexts:_*`, or the field
   cleared, finds no notes, then import cards.tsv.

After changing a template in anki/, run `make apkg` (the first run makes a
Python venv in .venv with genanki) and import anki/zhread.apkg again: the
note type keeps a fixed ID, so Anki updates it in place. A change to the
fields or to the card types or their order changes the note type's schema,
and Anki then imports it as a second note type, unless "Merge note types" is
ticked in the import dialog. Contexts and ContextsTrad were such a change,
added as the last two fields after the first cards were made: the cards
from before them have the two columns of cards.tsv empty.

Each time a card comes up, it shows the next of its contexts: the one made with
it, then those added to it by later cards for the word, in the card's order,
held in the Contexts and ContextsTrad fields as one div each. The front stores
which it showed in the webview's localStorage, keyed by card type and word, and
the back shows the same one. Where that storage does not last from one session
to the next, the turn starts at a random context. A cloze card blanks the other
contexts as its own is blanked, in the card's script.

The recognition card shows the context with the word in bold, wherever it
has it, and asks for the reading and meaning. The cloze card shows the
context with the word blanked, every place it has it, and the definitions
below, and asks for the word. The definitions have the word blanked too,
since MoE's examples often quote it, and then each of its characters, a ＿
apiece, as 天和地 in a definition of 天地 would give half of it away. When a note
has cards in both scripts, the cloze blank says which it asks for, [简体] or
[繁體]. English definitions sit behind a fold on both. The 繁 cards are made
only for notes with a Traditional field, that is, where the two scripts
differ, and come first in the note type, so Anki introduces a note's 繁 cards
before its 简 ones.

The answer gives the word as the pane does, 組合（组～）, then the reading and zhuyin
and the definitions. Sizes are those of the cards from before zhread
(anki/style.css): the text 40px, the context 60px, the word 80px. The
traditional cards are in a Kai, as those were: the Ministry of Education's
standard Kai, TW-MOE-Std-Kai, where it is installed or added to Anki's media
folder as `_edukai.ttf` (loaded with `@font-face`, which syncs it to phones;
the README's "Optional: the Kai font" tells how), else a Kai the system has
(DFKai-SB, BiauKai, Kaiti TC, AR PL UKai TW), else the serif the simplified
cards use. Those are in a serif font (Source Han Serif or Noto Serif CJK where
installed, else the system's serif). The text is all one color but for the word
in the context and the cloze blank, which are orange: in Anki's dark mode the
orange of the reader's headwords, Zenburn's #ffd7af, and in its light mode, on
white, a darker #af5f00, since #ffd7af is too pale to read there (1.35:1
against white, where #af5f00 is 4.7:1).

## Taiwan and Mainland readings

MoEDict gives Taiwan readings and CC-CEDICT Mainland ones; 兩岸詞典 and
Wiktionary give both. Where the two differ, the Mainland reading is
shown, and with `t` the Taiwan one noted after it, on every line for the
word whichever dictionary it came from: `qǐ yè (TW qì yè)`. `-pron tw`
shows the Taiwan reading instead, or first: `qì yè (CN qǐ yè)`.

The differences come from the cross-strait dictionary (兩岸詞典), which gives both
readings for every entry where they differ; dict/csld_pron.tsv (built with
`--csld`) holds the 6,443 of its rows that differ in pronunciation rather than
only in notation (see [dict/README.md](dict/README.md)). For words it lacks,
dict/wikt_pron.tsv (built with `--wikt`) adds 2,269 pairs from Wiktionary, such
as 導播 dǎo bò against dǎo bō. A reading is looked up in the column for the side
its dictionary speaks for, and in the other column too for MoEDict, which
sometimes already gives the Mainland reading (認識 rèn shi). CC-CEDICT's own
"Taiwan pr. [...]" note, when it ends a definition, moves into the reading; one
inside parentheses applies to a single sense and stays where it is.

The table's rows are per sense but do not say which sense, so a row is not
applied where it could belong to another entry. A Taiwan reading that
CC-CEDICT also gives the word is left alone: MoEDict's 模 mó "model" is
mó on the Mainland too, and the table's 模 mó → mú is about 模 "mold".
Likewise a Mainland reading is not matched when the dictionary has the
row's other reading as an entry of its own: 和 hé "sum" is not the 和 hàn
"and" that the Mainland reads hé. Such entries show the reading their
dictionary gives, unmarked.

A word neither table has falls back on the two dictionaries themselves:
where MoEDict and CC-CEDICT each give it exactly one reading and the two
differ, MoEDict's is taken for Taiwan's and CC-CEDICT's for the
Mainland's, on both lines (一字不識 yī zì bù shí (TW yī zì bù shì)). This
applies to words of two or more characters that 兩岸詞典 does not have,
and to words a table has only rows for that neither reading matches
(薄伽丘, whose Wiktionary pair has a typo). Neutral tones count (保住 bǎo
zhù (TW bǎo zhu)). Of the 748 words it covers, each checked by hand, 28
are two senses rather than two standards (下咽 xià yàn "to swallow"
against xià yān "hypopharynx") and are listed in dict/fallback_exclude.tsv
instead. Single characters are left out because that is much commoner
among them (羧 zuī a woolen cloth, suō the carboxyl), and so are words
with several readings in either dictionary. Any other word shows the
reading its dictionary gives, unmarked.


## Segmentation

zhread divides text with `viterbi`, below, and with `bidi` if
dict/jieba/dict.txt cannot be read. With `-chars`, or after `s`, it divides
it into single characters instead (`char`), for classical Chinese, whose
words are mostly one character and which a segmenter counting modern words
joins wrongly (非常道 as 非常 / 道 "very" rather than 非 / 常 / 道 "not the
constant way"). `s` forgets the words found so far, in the book and in the
panes moved into, and they are found again in the new mode; a batch
segmented before the switch is dropped when it arrives. A selection is still
looked up as one word. The zhdict package's other four modes:

| mode    | segmentation                                                     |
| ------- | ---------------------------------------------------------------- |
| viterbi | best-scoring path under a unigram word-frequency model (default) |
| fmm     | forward maximum matching: longest word from the left             |
| bmm     | backward maximum matching: longest word from the right           |
| bidi    | both maximum matches, then the tie-break below                   |

Maximum matching takes the longest dictionary word starting at the current
position, then resumes after it, falling back to a single character where no
longer word is known. Run forward it errs in a predictable direction:
wherever a long word and a shorter one share a prefix it takes the long one,
so 研究生命起源 comes out as 研究生 / 命 / 起源. Run backward it makes a
different set of mistakes, and the two agree except where the run is
genuinely ambiguous. `bidi` runs both and prefers the result with fewer
words, then the one with fewer single-character words, then the backward
one.

Where both of those criteria tie, `bidi` is choosing arbitrarily, and
arbitrarily is sometimes wrong. 交流感 is 交流 / 感 forwards and 交 / 流感
backwards: two words either way, one single character either way, so the
backward reading wins only because it is the fallback, even though 流感 is a
much less common word than 交流.

`viterbi` scores the candidates instead. Each word is given the probability
count/total from dict/jieba/dict.txt, a segmentation scores the product over
its words, and the best path through the lattice of dictionary words is
taken. Every word carries a factor of 1/total, so the model penalizes using
more words on its own — bidi's first criterion arrived at from the counts
rather than asserted — and it reads 交流感 as 交流 / 感, 交流 being some 40
times commoner than 流感.

Only jieba's counts are per word. No per-character frequency can separate two
segmentations of one run: both readings of 交流感 are made of 交, 流 and 感, so any
score summed over the characters is the same for the two. A dictionary word the
table does not list, which is most of what the additions files carry, scores as
one occurrence — rarer than anything listed but still a word, so 露鋒方筆館閣體 stays
露鋒 / 方筆 / 館閣體 rather than coming apart into characters. Reading the table costs
about 17 ms a query here, taking 39 ms to 56 ms, the file being 4.9 MB against
the 49 MB of definition sources.

Runs are segmented between non-Chinese characters, which are dropped. A
full-width comma and an interpunct are exceptions, because 1,469 entries
have a comma in a headword (couplets and proverbs) and 197 have an
interpunct (transliterated names); both are skipped when no word covers
them.


## The demo

demo/reader.gif shows zhread reading Lu Xun's 《故鄉》 a word at a time, then
carding 禁不住 with its second sense, 控制不住 "can't help", rather than its first,
承受不住 "can't bear": once with the keys, each caption naming the arrow key
and then vi's, and once with the mouse.
`scripts/zhread_demo.sh` makes it, and is rerun to make it again after zhread
changes: it plays a list of actions (keys, clicks, double clicks, drags, the
wheel, captions, pauses) on a simulated screen with `gozhread -demo` and draws
each screen as a frame, with the mouse pointer from a mouse action until the
next keys, so it needs only Go and a font with Chinese in it, found with
fc-match (FONT=/path/to/font otherwise). A click names its place by the text
there, as `click save`, so the actions don't depend on where the text falls.
The actions are in the script; `gozhread -demo help` lists them. The text,
where it starts, how many words it moves over, the word carded and the sense
taken are variables at its top, which the environment overrides; they default
to what demo/reader.gif was made with:

```sh
scripts/zhread_demo.sh
FILE=book.txt START=第一句 STEPS=3 WORD=詞語 SENSE=2 scripts/zhread_demo.sh
```

The text is demo/guxiang.txt: 《故鄉》 (1921), in the public domain, from
Chinese Wikisource's transcription of the 1948 《魯迅全集》, with the edition's 鄕
written 鄉 as modern editions write it, so that 故鄉 is found as one word. A
modern text shows zhread as it is meant to be used: its segmenter and
dictionaries are for modern Mandarin, and in classical Chinese they join
characters that are separate words (非常 "not constant" in the 道德經 becomes
"very").

The cards the demo makes go to a scratch file, not your card file. CARDS names
a new file to save them to, from which demo/cards.gif is made: it shows them
as Anki does, each card's question and then its answer.

```sh
CARDS=/tmp/demo-cards.tsv scripts/zhread_demo.sh
make cards-gif CARDS=/tmp/demo-cards.tsv
```

`make cards-gif` runs `scripts/cards_demo.py`, which fills in anki/'s templates
as Anki does and draws them with Chromium, through Playwright, in Anki's dark
theme (`--light` for its light one). The first run installs
requirements-demo.txt and Playwright's Chromium into `.venv/`. The cards are
drawn in the fonts installed, not from Anki's media folder, so for them to look
as demo/cards.gif does, install TW-MOE-Std-Kai, the Ministry of Education's
Kai
(<https://language.moe.gov.tw/001/Upload/Files/site_content/M0001/edukai-5.0.zip>),
and Noto Serif CJK SC or Source Han Serif.
