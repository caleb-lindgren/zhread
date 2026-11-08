package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/caleb-lindgren/ankitools/internal/opencc"
	"github.com/caleb-lindgren/ankitools/internal/zhdict"
	"github.com/gdamore/tcell/v2"
)

const (
	normal = iota
	visual
	vline
	prompt
	card
	choose // which words of the selection to card
)

// drow is one display row: a slice of a line that fits the screen width,
// drawn from column x.
type drow struct{ line, start, end, x int }

// level is a text being read with its cursor: the book, or a definitions
// pane moved into with Tab, which then has a pane of its own below it.
type level struct {
	d        *doc
	rows     []drow
	firstRow []int // index into rows of each line's first row
	rw       int   // the width rows were wrapped to
	top      int   // first row on screen

	cl, cc  int // cursor line and column
	want    int // display column j and k aim for, from vcol
	al, ac  int // visual anchor
	mode    int
	pword   string // the word the pane was last scrolled for
	pscroll int    // rows of the pane scrolled past, J and K
	// bh is the rows the level takes on screen while a level below it
	// is read: the book's text, or a pane's, rule included, as tall as it
	// was as a pane.
	bh int
}

// block is where a level, or with lv nil the pane, has its text on screen.
type block struct {
	lv   *level
	y, n int // its first row and how many
}

type view struct {
	*level
	stack   []*level // the levels above, the book first
	blocks  []block  // where draw put each level's text and the pane's
	s       tcell.Screen
	dict    *zhdict.Dict
	rec     *record
	reqs    chan<- segReq
	conv    convs
	convDir string
	deck    string
	pron    string

	w, h   int
	ph     int  // rows of the definitions pane, rule included
	pfrom  int  // the mode the prompt was opened from
	pchar  rune // the prompt's kind: / to search, : for a command
	pend   rune // first key of a two-key command (g)
	count  int
	showEn bool
	showTW bool // also show the other reading: TW, or CN with -pron tw
	help   bool // the pane shows the keys instead of definitions
	expand bool // show MoEDict's quotations, in the pane and on cards
	moreZh bool // show MoEDict where 兩岸詞典 defines the word too, c
	moreEn bool // show Wiktionary where CC-CEDICT defines it too, E
	msg    string
	// marks are the toggles, of x c E, that would change what the pane
	// shows for the word, noted at the right of its rule.
	marks   string
	chelp   bool // the card shows its keys in a pane below it
	cscroll int  // rows of the card's keys scrolled past

	pat   *regexp.Regexp
	back  bool // last search went backward
	input []rune
	// shiftSel is set while the selection was begun with Shift or the
	// mouse, so that a move without Shift ends it.
	shiftSel bool
	// drag is set while the left button is held after a press on the
	// text, at dl, dc, so that moving it selects.
	drag   bool
	dl, dc int
	pick   *picker // the segmentations to choose from, in choose
	dr     *draft
	queue  []*draft // the selection's cards still to come after dr
	quit   bool
}

// Colours from vis's zenburn theme (lua/themes/zenburn.lua). Every one is
// in the xterm 256-colour palette, so a terminal without true colour, st
// among them, shows them exactly.
func hex(c int32) tcell.Color { return tcell.NewHexColor(c) }

// The vis style each comes from is named after it.
var (
	stDef = tcell.StyleDefault.Foreground(hex(0xd7d7d7)).
		Background(hex(0x3a3a3a)) // DEFAULT
	stCarded = stDef.Foreground(hex(0xf0dfaf)) // REGEX
	stSel    = stDef.Background(hex(0x5f875f)) // SELECTION
	// The cursor is white, which no vis style is; the rest of the word
	// under it takes the cursor's colours.
	stCursor = stDef.Foreground(hex(0x1c1c1c)).
			Background(hex(0xffffff)).Bold(true)
	stCurWord = stDef.Foreground(hex(0x1c1c1c)).
			Background(hex(0x87afaf)).Bold(true) // CURSOR_PRIMARY
	stHead   = stDef.Foreground(hex(0xffd7af)).Bold(true) // DEFINITION
	stPinyin = stDef.Foreground(hex(0xd7d7af))            // FUNCTION
	stHint   = stDef.Foreground(hex(0x87af87))            // COMMENT
	stRule   = stDef.Foreground(hex(0xffd7af))            // DEFINITION
	stPos    = stDef.Foreground(hex(0xd7afaf)).Bold(true) // CONSTANT
	stEx     = stDef.Foreground(hex(0x87d7d7))            // NUMBER
	stRel    = stDef.Foreground(hex(0xafaf87))            // KEYWORD
	stStatus = stDef.Foreground(hex(0xafaf87)).
			Background(hex(0x303030)).Bold(true) // STATUS_FOCUSED
	stWarn = stDef.Foreground(hex(0xd78787)).Bold(true) // STRING
	stWord = stHead.Underline(true)
)

func (v *view) run() error {
	for !v.quit {
		v.draw()
		v.requestSeg()
		switch ev := v.s.PollEvent().(type) {
		case *tcell.EventResize:
			v.s.Sync()
			v.rows = nil
		case *tcell.EventInterrupt:
			switch d := ev.Data().(type) {
			case segRes:
				d.doc.apply(d)
			case pipeDone:
				v.msg = string(d)
			}
		case *tcell.EventKey:
			v.key(ev)
		case *tcell.EventMouse:
			v.mouse(ev)
		}
	}
	return nil
}

// layout: the text above, the definitions pane below it, a status line.
// The pane is as tall as the current definitions need, up to half the
// screen; draw sets ph from them before laying out the text. Below the book,
// the text is a pane moved into, as tall as it was, with the levels above
// it stacked over it as far as they fit.
func (v *view) textH() int {
	if len(v.stack) == 0 {
		return v.h - 1 - v.ph
	}
	return max(min(v.bh, v.h-1-v.ph)-1, 1)
}

// wrap splits every line into display rows for the current width, a
// pane's lines wrapping back to their indent as the pane's did.
func (v *view) wrap() {
	w := v.w
	if v.rows != nil && w == v.rw {
		return
	}
	v.rw = w
	v.rows = v.rows[:0]
	v.firstRow = make([]int, len(v.d.lines)+1)
	for l, line := range v.d.lines {
		v.firstRow[l] = len(v.rows)
		x, wx := 0, 0
		if v.d.ind != nil {
			x, wx = v.d.ind[l][0], v.d.ind[l][1]
		}
		start, sx := 0, x
		for c, r := range line {
			rw := zhdict.Width(string(r))
			if x+rw > w && c > start && x > wx {
				v.rows = append(v.rows, drow{l, start, c, sx})
				start, x, sx = c, wx, wx
			}
			x += rw
		}
		v.rows = append(v.rows, drow{l, start, len(line), sx})
	}
	v.firstRow[len(v.d.lines)] = len(v.rows)
}

// rowOf is the index of the display row holding line l, column c.
func (v *view) rowOf(l, c int) int {
	i := v.firstRow[l]
	for i+1 < v.firstRow[l+1] && v.rows[i+1].start <= c {
		i++
	}
	return i
}

func (v *view) lineLen(l int) int { return len(v.d.lines[l]) }

// clamp keeps the cursor on a character.
func (v *view) clamp() {
	v.cl = min(max(v.cl, 0), len(v.d.lines)-1)
	v.cc = min(v.cc, v.lineLen(v.cl)-1)
	v.cc = max(v.cc, 0)
}

// scroll keeps the cursor's row a fixed number of rows from the top, so the
// text stays put as the definitions pane below it grows and shrinks; only
// near the start of the file is it higher. The row is the middle of the
// least text the pane leaves, a quarter of the way down; the keys, which
// may leave less, can cover it.
//
// A pane moved into scrolls only as far as keeps the cursor in view.
func (v *view) scroll() {
	ri := v.rowOf(v.cl, v.cc)
	if len(v.stack) == 0 {
		v.top = max(ri-v.anchor(), 0)
		return
	}
	th := v.textH()
	v.top = max(min(v.top, len(v.rows)-th), 0)
	v.top = min(max(v.top, ri-th+1), ri)
}

// anchor is how many rows below the top of the screen scroll keeps the
// cursor's row.
func (v *view) anchor() int { return (v.h - 1 - v.h/2) / 2 }

// requestSeg asks the worker to segment the lines around the screen that
// are not done yet, a screenful ahead and half of one behind.
func (v *view) requestSeg() {
	th := v.textH()
	a := v.rows[max(v.top-th/2, 0)].line
	b := v.rows[min(v.top+2*th, len(v.rows)-1)].line
	rq := segReq{doc: v.d}
	n := 0
	for l := a; l <= b && n < 20000; l++ {
		if v.d.toks[l] == nil && !v.d.pending[l] {
			rq.lines = append(rq.lines, l)
			rq.text = append(rq.text, v.d.lines[l])
			n += len(v.d.lines[l])
		}
	}
	if len(rq.lines) == 0 {
		return
	}
	select {
	case v.reqs <- rq:
		for _, l := range rq.lines {
			v.d.pending[l] = true
		}
	default: // the worker is busy; ask again after it reports
	}
}

func (v *view) draw() {
	v.w, v.h = v.s.Size()
	v.wrap()
	v.clamp()
	if v.mode != card && v.mode != choose {
		v.settle(v.cl, v.cc) // as on opening, onto a number
	}
	v.s.Clear()
	switch v.mode {
	case card:
		v.drawCard()
		v.s.Show()
		return
	case choose:
		v.drawChoose()
		v.s.Show()
		return
	}
	if w, _ := v.lookup(); w != v.pword {
		v.pword, v.pscroll = w, 0
	}
	pane := v.pane()
	v.ph = 0
	if v.h >= 6 {
		need := v.need(pane)
		// The keys may take all but a few rows of text; definitions,
		// half the screen.
		top := v.h / 2
		if v.help {
			top = v.h - 3
		}
		v.ph = min(need, top)
		// Below the book the pane keeps its full height however much
		// it shows, so the levels above it, and the cursor, stay put as
		// the cursor moves from word to word.
		if len(v.stack) > 0 && !v.help {
			v.ph = top
		}
	}
	v.scroll()
	v.blocks = v.blocks[:0]
	y := v.h - 1 - v.ph
	if len(v.stack) == 0 {
		v.drawBlock(v.level, 0, y, false, true)
	} else {
		n := v.textH() + 1
		y -= n
		v.drawBlock(v.level, y, n, true, true)
	}
	// The levels above, cut off at the top once they run out of room.
	for i := len(v.stack) - 1; i >= 0 && y > 0; i-- {
		lv := v.stack[i]
		y -= lv.bh
		v.drawBlock(lv, y, lv.bh, i > 0, false)
	}
	if v.ph > 0 {
		y := v.h - 1 - v.ph
		v.drawPane(y, v.ph, &v.pscroll, pane, v.marks)
		v.blocks = append(v.blocks, block{nil, y + 1, v.ph - 1})
	}
	v.s.HideCursor()
	v.drawStatus()
	v.s.Show()
}

// drawBlock draws a level's text in n rows from y, under a rule when it is
// a pane moved into. Rows above the screen are not drawn. Only the level
// being read, active, has a cursor; the rest mark the word they looked up.
func (v *view) drawBlock(lv *level, y, n int, rule, active bool) {
	cur := v.level
	v.level = lv
	defer func() { v.level = cur }()
	v.wrap()
	v.top = max(min(v.top, len(v.rows)-1), 0)
	if rule {
		if y >= 0 {
			v.rule(y, "", v.top, len(v.rows)-v.top-(n-1))
		}
		y, n = y+1, n-1
	}
	v.blocks = append(v.blocks, block{lv, y, n})
	for k := 0; k < n && v.top+k < len(v.rows); k++ {
		if y+k >= 0 {
			v.drawRow(y+k, v.rows[v.top+k], active)
		}
	}
}

// need is the rows a pane takes to show all of lines, its rule included.
func (v *view) need(lines []styled) int {
	n := 1
	for _, l := range lines {
		n += v.height(l.x, l.wx, l.text)
	}
	return n
}

func (v *view) drawRow(y int, r drow, active bool) {
	line := v.d.lines[r.line]
	hl := map[int]bool{}
	if v.d.toks[r.line] != nil && v.d.sty == nil {
		for _, t := range v.d.toks[r.line] {
			if t.han && t.end > r.start && t.start < r.end &&
				v.rec.words[string(line[t.start:t.end])] {
				for c := t.start; c < t.end; c++ {
					hl[c] = true
				}
			}
		}
	}
	here := r.line == v.cl
	cursor := here && active && (v.mode != prompt || v.pchar == ':')
	if cursor && len(line) == 0 {
		v.s.SetContent(r.x, y, ' ', nil, stCursor)
	}
	_, word := v.lookupTok()
	x, lx := r.x, r.x
	for c := r.start; c < r.end; c++ {
		st := stDef
		if v.d.sty != nil {
			st = v.d.sty[r.line][c]
		}
		if hl[c] {
			st = stCarded
		}
		if v.selected(r.line, c) {
			st = stSel
		} else if here && (cursor || !active) && !v.selecting() &&
			word.start <= c && c < word.end {
			st = stCurWord
		}
		if cursor && c == v.cc {
			st = stCursor
		}
		if rw := zhdict.Width(string(line[c])); rw == 0 && x > r.x {
			v.combine(lx, y, line[c])
			continue
		}
		v.s.SetContent(x, y, line[c], nil, st)
		lx = x
		x += zhdict.Width(string(line[c]))
	}
}

// put writes text from (x, y), wrapping at the screen edge back to
// column x, and returns the row after it. Rows at or past maxY are not
// drawn. style gives each rune's style.
func (v *view) put(x, y, maxY int, text []rune,
	style func(i int) tcell.Style) int {
	return v.putw(x, x, y, 0, maxY, text, style)
}

// putw is put wrapping back to column wx, and drawing only the rows from
// minY, so text can be scrolled partly out of view at the top.
func (v *view) putw(x, wx, y, minY, maxY int, text []rune,
	style func(i int) tcell.Style) int {
	cx, lx := x, -1
	for i, r := range text {
		rw := zhdict.Width(string(r))
		if rw == 0 {
			if y >= minY && y < maxY && lx >= 0 {
				v.combine(lx, y, r)
			}
			continue
		}
		if cx+rw > v.w && cx > wx {
			cx, y = wx, y+1
		}
		if y >= maxY {
			return y
		}
		if y >= minY {
			v.s.SetContent(cx, y, r, nil, style(i))
		}
		lx = cx
		cx += rw
	}
	return y + 1
}

// combine adds a combining mark, as the dot below in 兩岸詞典's 7̣, to the
// character drawn at x, y. Drawn as a character of its own, a mark of no
// width would take that one's place.
func (v *view) combine(x, y int, r rune) {
	mainc, comb, st, _ := v.s.GetContent(x, y)
	v.s.SetContent(x, y, mainc, append(comb, r), st)
}

// height is how many rows putw would take for text.
func (v *view) height(x, wx int, text []rune) int {
	cx, n := x, 1
	for _, r := range text {
		rw := zhdict.Width(string(r))
		if cx+rw > v.w && cx > wx {
			cx, n = wx, n+1
		}
		cx += rw
	}
	return n
}

func plain(st tcell.Style) func(int) tcell.Style {
	return func(int) tcell.Style { return st }
}

// lookup is the word the definitions pane shows: the selection, or the
// word under the cursor, with the probe that can look it up.
func (v *view) lookup() (string, *zhdict.Probe) {
	if v.selecting() {
		l1, c1, l2, c2 := v.selRange()
		if l1 != l2 {
			return "", nil
		}
		return string(v.d.lines[l1][c1 : c2+1]), v.d.probe[l1]
	}
	for _, t := range v.d.lineToks(v.cl) {
		if t.han && t.start <= v.cc && v.cc < t.end {
			return string(v.d.lines[v.cl][t.start:t.end]),
				v.d.probe[v.cl]
		}
	}
	return "", nil
}

// styled is a line of text from column x, wrapping back to column wx,
// with a style for each rune.
type styled struct {
	x, wx int
	text  []rune
	style func(int) tcell.Style
	// lead and tail are the runes at either end that are not definition
	// text, as the word, its reading and the notes on it, which the
	// cursor passes over in a pane moved into.
	lead, tail int
}

// piece is a run of text in one style.
type piece struct {
	text string
	st   tcell.Style
}

// line joins pieces into one styled line from column x.
func line(x int, ps ...piece) styled {
	var text []rune
	var ends []int
	var sts []tcell.Style
	for _, p := range ps {
		text = append(text, []rune(p.text)...)
		ends, sts = append(ends, len(text)), append(sts, p.st)
	}
	return styled{x: x, wx: x, text: text, style: func(i int) tcell.Style {
		for k, e := range ends {
			if i < e {
				return sts[k]
			}
		}
		return stDef
	}}
}

// runes is how many runes pieces hold.
func runes(ps []piece) int {
	n := 0
	for _, p := range ps {
		n += utf8.RuneCountInString(p.text)
	}
	return n
}

func width(ps []piece) int {
	n := 0
	for _, p := range ps {
		n += zhdict.Width(p.text)
	}
	return n
}

// pane is what the definitions pane shows below its rule: each definition
// starting on a line with the word and its reading, every line of it
// indented to where it starts; or the keys.
func (v *view) pane() []styled {
	v.marks = ""
	if v.help {
		return helpLines()
	}
	word, p := v.lookup()
	if word == "" {
		return nil
	}
	if p == nil {
		return []styled{line(0, piece{"…", stHint})}
	}
	g := p.Rows(word)
	if g.Notice != "" {
		return []styled{line(0, piece{g.Notice, stHint})}
	}
	defer func() { v.marks = v.toggles(g) }()
	carded := func(sn zhdict.Sense) bool {
		return v.rec.cardedSense(word, sn)
	}
	// rows are the definitions shown: English on e, or at once when
	// there is no Chinese.
	var rows []int
	for i := range g.Rows {
		if i >= g.Zh && !v.showEn && g.Zh > 0 {
			break
		}
		if v.shows(g, i) {
			rows = append(rows, i)
		}
	}
	// A carded word none of whose senses shown is on a card, as one
	// carded from another dictionary, is noted so under the word.
	noted := v.rec.words[word] && !slices.ContainsFunc(rows,
		func(i int) bool {
			return slices.ContainsFunc(g.Senses(i), carded)
		})
	var out []styled
	for _, i := range rows {
		r := g.Rows[i]
		head := []piece{{headword(r[0], r[1]), stHead}}
		if i < len(g.Variant) && g.Variant[i] != "" {
			head = append(head, piece{" (" + g.Variant[i] + ")",
				stHint})
		}
		head = append(head, piece{"  " + v.reading(r[2]), stPinyin},
			piece{"  ", stDef})
		col := min(width(head), v.w/2)
		ls := v.senseLines(g.Senses(i), col, carded, i >= g.Zh)
		if len(ls) == 0 {
			ls = []sline{{x: col, wx: col}}
		}
		// Notes on the whole definition go under the word.
		var notes []string
		if g.Extra[i] {
			notes = append(notes, g.Source(i))
		}
		if len(out) == 0 && noted {
			notes = append(notes, "(carded)")
		}
		for k, l := range ls {
			sl := line(l.x, l.ps...)
			sl.lead = l.lead
			if k == 0 {
				sl = line(0, append(head, l.ps...)...)
				sl.lead = runes(head) + l.lead
			}
			sl.wx, sl.tail = l.wx, l.tail
			out = append(out, sl)
		}
		if len(notes) > 0 {
			note := piece{strings.Join(notes, "  "), stHint}
			first := len(out) - len(ls)
			if len(ls) > 1 && zhdict.Width(note.text) < ls[1].x {
				// in the gap left of the second line
				pad := piece{strings.Repeat(" ",
					ls[1].x-zhdict.Width(note.text)), stDef}
				sl := line(0, append([]piece{note, pad},
					ls[1].ps...)...)
				sl.wx, sl.tail = ls[1].wx, ls[1].tail
				sl.lead = runes([]piece{note, pad}) + ls[1].lead
				out[first+1] = sl
			} else {
				sl := line(0, note)
				sl.lead = len(sl.text)
				out = slices.Insert(out, first+1, sl)
			}
		}
	}
	return out
}

// toggles are those of x, c, e and E that would change what the pane
// shows of g: x when a definition shown has quotations it hides (a sense
// that is only quotations shows them anyway), c when MoEDict has a
// definition hidden or shown beside 兩岸詞典, e when there are English
// definitions after Chinese ones, and E when Wiktionary has one beside
// CC-CEDICT, while the English is shown.
func (v *view) toggles(g zhdict.Group) string {
	en := v.showEn || g.Zh == 0
	var x, c, e, E bool
	for i := range g.Rows {
		if i >= g.Zh && g.Zh > 0 {
			e = true
		}
		if g.Extra[i] && i < g.Zh {
			c = true
		} else if g.Extra[i] && en {
			E = true
		}
		if x || i >= g.Zh && !en || !v.shows(g, i) {
			continue
		}
		x = slices.ContainsFunc(g.Senses(i),
			func(sn zhdict.Sense) bool {
				return len(sn.Cites) > 0 &&
					(sn.Text != "" || len(sn.Ex) > 0)
			})
	}
	return marks(x, c, e, E)
}

// marks notes which of x, c, e and E have anything to show or hide.
func marks(x, c, e, E bool) string {
	var out []string
	for _, t := range []struct {
		on bool
		k  string
	}{{x, "x"}, {c, "c"}, {e, "e"}, {E, "E"}} {
		if t.on {
			out = append(out, t.k)
		}
	}
	return strings.Join(out, " ")
}

// cardToggles are those of x, c and E that would change the card: x when
// a definition ticked has quotations, c when MoEDict has a definition
// beside 兩岸詞典, and E when Wiktionary has one beside CC-CEDICT. e is
// never one, as the card lists its English definitions whatever e says.
func (v *view) cardToggles() string {
	dr := v.dr
	var x, c, E bool
	for i, o := range dr.opts {
		x = x || dr.sel[i] && len(o.s.Cites) > 0
	}
	for i := range dr.g.Rows {
		if dr.g.Extra[i] && i < dr.g.Zh {
			c = true
		} else if dr.g.Extra[i] {
			E = true
		}
	}
	return marks(x, c, false, E)
}

// shows reports whether row i of g is shown: a fallback dictionary's
// definition of a word the main one defines is shown only on c (MoEDict,
// for Chinese) or E (Wiktionary, for English).
func (v *view) shows(g zhdict.Group, i int) bool {
	if !g.Extra[i] {
		return true
	}
	if i < g.Zh {
		return v.moreZh
	}
	return v.moreEn
}

// label matches the usage labels of 兩岸詞典 (〈書〉, 〈口〉, 〈大陸〉), and those
// MoEDict (諺語) and Wiktionary (literary) start a sense with, which are
// shown in the part-of-speech colour. A sub-sense number, (1), is not one.
var label = regexp.MustCompile(`〈[^〈〉]{1,4}〉|^\([^()0-9][^()]{0,40}\)`)

// sline is a line of a definition: its pieces from column x, wrapping to
// column wx.
type sline struct {
	x, wx      int
	ps         []piece
	lead, tail int // as in styled
}

// maxHang is the widest part of speech that senses hang after; a wider one,
// like Wiktionary's <character>, has a line of its own.
const maxHang = 10

// senseLines lays out a definition's senses from column col, the first
// line's to follow the word and its reading. A part of speech starts the
// first sense after it, and that sense and the rest under it line up after
// the part of speech:
//
//	的  de  <助> ① 結構助詞：
//	               (1) 置於形容詞後。如：「美麗的風景」。
//	             ② 句尾助詞：置於句尾，…
//
// A heading over sub-senses has a line of its own, with the sub-senses, and
// any heading among them, indented under it. Quotations, shown on x, are
// indented under their sense. A sense carded reports as on a card is
// marked so at the end of its last line, after a space if the definition
// is English. A sense's numbering is passed over by the cursor, as the
// word and reading are.
func (v *view) senseLines(senses []zhdict.Sense, col int,
	carded func(zhdict.Sense) bool, en bool) []sline {
	var out []sline
	add := func(x int, pre []piece, ps ...piece) {
		lead := runes(pre)
		if len(ps) > 0 {
			lead += numLen([]rune(ps[0].text))
		}
		out = append(out, sline{x: x - width(pre), wx: x,
			ps: append(pre, ps...), lead: lead})
	}
	pos, x := "", col
	var heads []string
	for k, sn := range senses {
		var pre []piece
		if k == 0 || sn.Pos != pos {
			pos, heads, x = sn.Pos, nil, col
			if w := zhdict.Width(pos) + 1; pos == "" {
			} else if w <= maxHang && col+w <= v.w/2 {
				pre, x = []piece{{pos + " ", stPos}}, col+w
			} else {
				add(col, nil, piece{pos, stPos})
				out[len(out)-1].lead = runes([]piece{{pos, stPos}})
				x = col + 2
			}
		}
		// The headings the sense before was not also under.
		j := 0
		for j < min(len(heads), len(sn.Heads)) &&
			heads[j] == sn.Heads[j] {
			j++
		}
		for ; j < len(sn.Heads); j++ {
			add(x+2*j, pre, labelled(sn.Heads[j])...)
			pre = nil
		}
		heads = sn.Heads
		sx := x + 2*len(heads)
		add(sx, pre, sensePieces(sn)...)
		// A sense that is only quotations, as some of MoEDict's are,
		// shows them anyway.
		if v.expand || sn.Text == "" && len(sn.Ex) == 0 {
			for _, c := range sn.Cites {
				add(sx+2, nil, piece{c, stHint})
			}
		}
		if carded != nil && carded(sn) {
			note := piece{"(carded)", stHint}
			if en {
				note.text = " " + note.text
			}
			last := &out[len(out)-1]
			last.ps = append(last.ps, note)
			last.tail = runes([]piece{note})
		}
	}
	return out
}

// headword is a word as the pane shows it, and a card's answer: the
// traditional form, and when the simplified one differs, it after in
// parentheses with the characters the two share as ～, 組合（组～）.
func headword(trad, simp string) string {
	if simp == "" || simp == trad {
		return trad
	}
	t, s := []rune(trad), []rune(simp)
	if len(t) == len(s) {
		for i := range s {
			if s[i] == t[i] {
				s[i] = '～'
			}
		}
	}
	return trad + "（" + string(s) + "）"
}

// labelled is text with its usage labels in the part-of-speech colour.
func labelled(t string) []piece {
	var ps []piece
	at := 0
	for _, m := range label.FindAllStringIndex(t, -1) {
		ps = append(ps, piece{t[at:m[0]], stDef},
			piece{t[m[0]:m[1]], stPos})
		at = m[1]
	}
	return append(ps, piece{t[at:], stDef})
}

// sensePieces is a sense on one line: its text, examples and synonym
// lists. The quotations are left to the caller, which shows them only on x.
func sensePieces(sn zhdict.Sense) []piece {
	var ps []piece
	if sn.Text != "" {
		ps = labelled(sn.Text)
	}
	add := func(t string, st tcell.Style) {
		if len(ps) > 0 {
			t = " " + t
		}
		ps = append(ps, piece{t, st})
	}
	for _, e := range sn.Ex {
		add(e, stEx)
	}
	for _, r := range sn.Rels {
		add(r, stRel)
	}
	return ps
}

// reading is a definition's reading, without the other side's unless t
// has asked for it.
func (v *view) reading(py string) string {
	if v.showTW {
		return py
	}
	return zhdict.Preferred(py)
}

// keyHelp is what gh shows in the pane.
var keyHelp = [][2]string{
	{"h j k l", "character left, line down, line up, right"},
	{"w b", "next word, previous word"},
	{"0 $  g0 g$", "start, end of the line; of the screen row"},
	{"gj gk", "screen row down, up"},
	{"gg G", "first line, last line (or line N with a count)"},
	{"^D ^U ^F ^B", "half page down, up; page down, up"},
	{"/ ?  n N", "search forward, back (a Go regexp); repeat"},
	{"* #", "search for the word under the cursor"},
	{"v V", "select by character, by line"},
	{"Enter", "make a card of the word, or of the selection"},
	{":>cmd", "in v or V: pipe the selection to a shell command"},
	{"m '", "bookmark this line, go to the bookmark"},
	{"e", "show or hide English definitions"},
	{"x", "show or hide quotations (on cards too)"},
	{"c", "show or hide MoEDict beside 兩岸詞典"},
	{"E", "show or hide Wiktionary beside CC-CEDICT"},
	{"t", "show or hide the other reading (Taiwan)"},
	{"J K", "scroll the definitions down, up"},
	{"Tab S-Tab", "move into the definitions; back up"},
	{"gh F1", "show this help"},
	{"Esc", "hide it; end a selection; back to the book"},
	{"q", "quit"},
	{"← →  ^← ^→", "character left, right; word back, forward"},
	{"↑ ↓  ^↑ ^↓", "screen row up, down; paragraph back, forward"},
	{"Home End", "start, end of the screen row; with ^, of the text"},
	{"PgUp PgDn", "page up, down"},
	{"Shift-", "with any of the last four: select as it moves"},
	{"M-", "Alt (Option) as Ctrl with the arrows; M-f M-b words"},
	{"click wheel", "move the cursor there; scroll the text"},
	{"drag", "select from where the button was pressed"},
}

// cardKeyHelp is what gh shows below a card.
var cardKeyHelp = [][2]string{
	{"j k", "move over the contexts and definitions"},
	{"PgDn PgUp", "the same, ten at a time"},
	{"space", "tick the definition or context, or untick it"},
	{"[ ] { }", "move the context's left edge, right edge"},
	{"i", "edit the context under the cursor"},
	{"Enter y", "save the card (again if already carded)"},
	{"u", "edit the word's card instead (1-9: which)"},
	{"< >", "move the context up, down the card's order"},
	{"n", "skip to the selection's next card"},
	{"Esc q", "cancel the card and those after it"},
	{"Esc Esc", "the same, asking first if it is edited"},
	{"x", "show or hide quotations (on the card too)"},
	{"c", "show or hide MoEDict beside 兩岸詞典"},
	{"E", "show or hide Wiktionary beside CC-CEDICT"},
	{"J K", "scroll these keys down, up"},
	{"gh F1", "show or hide these keys; Esc hides them"},
	{"", "In the editor on a context:"},
	{"h l w b e", "move by character, by word"},
	{"0 ^ $", "move to the start, first character, end"},
	{"i a I A", "insert before, after; at the start, end"},
	{"x X r s", "delete, delete back, replace, substitute"},
	{"d c D C", "delete, change over a motion; to the end"},
	{"dd cc u", "delete, change it all; undo"},
	{"Esc j k", "out of insert mode; back to the list"},
}

func helpLines() []styled { return keyLines(keyHelp) }

// keyLines lists keys and what they do, as gh shows them.
func keyLines(keys [][2]string) []styled {
	w := 12
	for _, k := range keys {
		w = max(w, zhdict.Width(k[0])+1)
	}
	var out []styled
	for _, k := range keys {
		if k[0] == "" {
			// a heading, after a blank line
			out = append(out, line(0),
				line(0, piece{k[1], stHint}))
			continue
		}
		pad := strings.Repeat(" ", w-zhdict.Width(k[0]))
		l := line(0, piece{k[0] + pad, stHead}, piece{k[1], stDef})
		l.wx = w
		out = append(out, l)
	}
	return out
}

// drawPane draws the rule and a pane's lines below it, ph rows in all from
// y0, scrolled by *scroll rows, with marks and how many rows are out of
// view above and below noted at the right of the rule.
func (v *view) drawPane(y0, ph int, scroll *int, lines []styled,
	marks string) {
	total := 0
	for _, l := range lines {
		total += v.height(l.x, l.wx, l.text)
	}
	shown := ph - 1
	*scroll = max(min(*scroll, total-shown), 0)
	v.rule(y0, marks, *scroll, total-shown-*scroll)
	y := y0 + 1 - *scroll
	for _, l := range lines {
		y = v.putw(l.x, l.wx, y, y0+1, y0+ph, l.text, l.style)
	}
}

// rule draws the rule over a pane at row y, noting at its right marks,
// and how many rows are out of view above and below.
func (v *view) rule(y int, marks string, above, below int) {
	for x := 0; x < v.w; x++ {
		v.s.SetContent(x, y, '─', nil, stRule)
	}
	var more []string
	if above > 0 {
		more = append(more, fmt.Sprintf("↑%d", above))
	}
	if below > 0 {
		more = append(more, fmt.Sprintf("↓%d", below))
	}
	// the marks set off from the counts by a second space
	if marks != "" {
		more = append([]string{marks + " "}, more...)
		more[len(more)-1] = strings.TrimSuffix(more[len(more)-1], " ")
	}
	if len(more) > 0 {
		m := " " + strings.Join(more, " ") + " "
		v.put(max(v.w-zhdict.Width(m)-1, 0), y, y+1, []rune(m),
			plain(stRule))
	}
}

func (v *view) drawStatus() {
	y := v.h - 1
	left := v.msg
	switch v.mode {
	case prompt:
		left = string(v.pchar) + string(v.input)
	case visual:
		left = "-- VISUAL --  Enter: make a card  :>cmd: pipe it"
	case vline:
		left = "-- VISUAL LINE --  Enter: make a card  :>cmd: pipe it"
	}
	right := fmt.Sprintf(" %d/%d  %s", v.cl+1, len(v.d.lines), v.d.name)
	for x := 0; x < v.w; x++ {
		v.s.SetContent(x, y, ' ', nil, stStatus)
	}
	v.put(0, y, y+1, []rune(left), plain(stStatus))
	rx := v.w - zhdict.Width(right)
	v.put(max(rx, 0), y, y+1, []rune(right), plain(stStatus))
	if v.mode == prompt {
		v.s.ShowCursor(zhdict.Width(left), y)
	}
}

// selMode is the visual mode a selection is shown in, also while a
// prompt opened from it is open.
func (v *view) selMode() int {
	if v.mode == prompt {
		return v.pfrom
	}
	return v.mode
}

func (v *view) selecting() bool {
	return v.selMode() == visual || v.selMode() == vline
}

// selRange is the selection, start before end, end column inclusive.
func (v *view) selRange() (l1, c1, l2, c2 int) {
	l1, c1, l2, c2 = v.al, v.ac, v.cl, v.cc
	if l2 < l1 || l2 == l1 && c2 < c1 {
		l1, c1, l2, c2 = l2, c2, l1, c1
	}
	if v.selMode() == vline {
		c1, c2 = 0, max(v.lineLen(l2)-1, 0)
	}
	c2 = min(c2, max(v.lineLen(l2)-1, 0))
	return
}

func (v *view) selected(l, c int) bool {
	if !v.selecting() {
		return false
	}
	l1, c1, l2, c2 := v.selRange()
	if l < l1 || l > l2 {
		return false
	}
	return (l > l1 || c >= c1) && (l < l2 || c <= c2)
}

func (v *view) key(ev *tcell.EventKey) {
	l0, c0 := v.cl, v.cc
	defer func() {
		if v.mode != card && v.mode != choose {
			v.settle(l0, c0)
		}
	}()
	switch v.mode {
	case prompt:
		v.promptKey(ev)
		return
	case card:
		v.cardKey(ev)
		return
	case choose:
		v.chooseKey(ev)
		return
	}
	v.msg = ""
	n := max(v.count, 1)
	switch ev.Key() {
	case tcell.KeyEscape:
		// Esc ends a selection or the keys; with neither, it goes back
		// from the panes moved into to the book.
		if v.mode == normal && !v.help && v.pend == 0 {
			for len(v.stack) > 0 {
				v.ascend()
			}
		}
		v.mode, v.pend, v.count, v.help = normal, 0, 0, false
		return
	case tcell.KeyF1:
		v.toggleHelp()
	case tcell.KeyTab:
		v.pend = 0
		v.descend()
	case tcell.KeyBacktab:
		v.pend = 0
		v.ascend()
	case tcell.KeyEnter:
		v.pend, v.count = 0, 0
		v.openCard()
		return
	case tcell.KeyCtrlD:
		v.byRows(n * v.textH() / 2)
	case tcell.KeyCtrlU:
		v.byRows(-n * v.textH() / 2)
	case tcell.KeyCtrlF:
		v.byRows(n * v.textH())
	case tcell.KeyCtrlB:
		v.byRows(-n * v.textH())
	case tcell.KeyLeft, tcell.KeyRight, tcell.KeyUp, tcell.KeyDown,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyPgUp, tcell.KeyPgDn:
		v.pend = 0
		v.navKey(ev, n)
	case tcell.KeyRune:
		// Esc b and Esc f, which tcell gives as Alt-b and Alt-f, are
		// word left and right, as iTerm2 sends them for Option-← and
		// Option-→ unless its profile says otherwise; Esc B and Esc F
		// select as they go.
		r := ev.Rune()
		if ev.Modifiers()&tcell.ModAlt != 0 &&
			strings.ContainsRune("bfBF", r) {
			k, mod := tcell.KeyLeft, tcell.ModAlt
			if r == 'f' || r == 'F' {
				k = tcell.KeyRight
			}
			if r == 'B' || r == 'F' {
				mod |= tcell.ModShift
			}
			v.pend = 0
			v.navKey(tcell.NewEventKey(k, 0, mod), n)
			break
		}
		v.runeKey(r)
		return
	}
	v.count = 0
}

// navKey moves the cursor n times as the arrow keys, Home, End, Page Up and
// Page Down do in an editor: by character, word, screen row, paragraph, to the
// ends of the row or the text, by page; Ctrl or Alt goes further. Shift
// selects from where the cursor was; a selection so begun, or by dragging the
// mouse, ends at the next move without Shift, as an editor's does, while one
// begun with v or V stays, as in vi.
func (v *view) navKey(ev *tcell.EventKey, n int) {
	mod := ev.Modifiers()
	// Alt (Option) as well as Ctrl: macOS's terminals keep Ctrl-arrows
	// for Mission Control and Cmd for themselves.
	ctrl := mod&(tcell.ModCtrl|tcell.ModAlt) != 0
	if mod&tcell.ModShift != 0 {
		if v.mode == normal {
			v.al, v.ac, v.mode = v.cl, v.cc, visual
			v.shiftSel = true
		}
	} else if v.shiftSel {
		if v.mode == visual {
			v.mode = normal
		}
		v.shiftSel = false
	}
	switch k := ev.Key(); {
	case k == tcell.KeyLeft && ctrl:
		for ; n > 0; n-- {
			v.wordBack()
		}
	case k == tcell.KeyRight && ctrl:
		for ; n > 0; n-- {
			v.wordFwd()
		}
	case k == tcell.KeyLeft || k == tcell.KeyRight:
		for ; n > 0; n-- {
			v.charStep(k == tcell.KeyRight)
		}
	case k == tcell.KeyUp && ctrl:
		for ; n > 0; n-- {
			v.paraBack()
		}
	case k == tcell.KeyDown && ctrl:
		for ; n > 0; n-- {
			v.paraFwd()
		}
	case k == tcell.KeyUp:
		v.byRows(-n)
		return // keeping the column it aimed for
	case k == tcell.KeyDown:
		v.byRows(n)
		return
	case k == tcell.KeyPgUp:
		v.byRows(-n * v.textH())
		return
	case k == tcell.KeyPgDn:
		v.byRows(n * v.textH())
		return
	case k == tcell.KeyHome && ctrl:
		v.cl, v.cc = 0, 0
	case k == tcell.KeyEnd && ctrl:
		v.cl = len(v.d.lines) - 1
		v.cc = max(v.lineLen(v.cl)-1, 0)
	case k == tcell.KeyHome:
		v.cc = v.rows[v.rowOf(v.cl, v.cc)].start
	case k == tcell.KeyEnd:
		row := v.rows[v.rowOf(v.cl, v.cc)]
		v.cc = max(row.end-1, row.start)
	}
	v.want = v.vcol()
}

// charStep moves the cursor a character right, or left, going on to the
// next line's start or the last line's end at the end of a line.
func (v *view) charStep(right bool) {
	switch {
	case right && v.cc < v.lineLen(v.cl)-1:
		v.cc++
	case right && v.cl < len(v.d.lines)-1:
		v.cl, v.cc = v.cl+1, 0
	case !right && v.cc > 0:
		v.cc--
	case !right && v.cl > 0:
		v.cl--
		v.cc = max(v.lineLen(v.cl)-1, 0)
	}
}

// paraStart reports whether line l starts a paragraph: a line after a
// blank one, as the book's paragraphs are taken for contexts, or in a
// pane, any line, each being a sense.
func (v *view) paraStart(l int) bool {
	return !blank(v.d.lines[l]) &&
		(l == 0 || v.d.sty != nil || blank(v.d.lines[l-1]))
}

// paraFwd moves to the start of the next paragraph, or the end of the
// text after the last.
func (v *view) paraFwd() {
	for l := v.cl + 1; l < len(v.d.lines); l++ {
		if v.paraStart(l) {
			v.cl, v.cc = l, 0
			return
		}
	}
	v.cl = len(v.d.lines) - 1
	v.cc = max(v.lineLen(v.cl)-1, 0)
}

// paraBack moves to the start of the paragraph, or from there to the
// start of the one before. The start is the first text the cursor can go
// to, past a number the line starts with.
func (v *view) paraBack() {
	l := v.cl
	if first, _ := v.d.textNear(l, 0, 1); v.paraStart(l) &&
		v.cc <= first {
		l--
	}
	for ; l >= 0; l-- {
		if v.paraStart(l) {
			v.cl, v.cc = l, 0
			return
		}
	}
	v.cl, v.cc = 0, 0
}

// mouse moves the cursor to a character clicked with the left button,
// and scrolls the text a row a notch with the wheel. A click in the pane
// moves into it, as Tab does, and one in a level above goes back up to it,
// as Shift-Tab does. A click ends a selection, and dragging with the button
// held selects from where it was pressed, as far as the level's text on
// screen goes. A click on a rule or the status line, or while a prompt or
// card is open, does nothing.
func (v *view) mouse(ev *tcell.EventMouse) {
	if v.mode == prompt || v.mode == card || v.mode == choose {
		v.drag = false
		return
	}
	x, y := ev.Position()
	switch b := ev.Buttons(); {
	case b&tcell.WheelDown != 0:
		v.byRows(1)
	case b&tcell.WheelUp != 0:
		v.byRows(-1)
	case b&tcell.Button1 != 0 && v.drag:
		for _, bl := range v.blocks {
			if bl.lv != v.level {
				continue
			}
			y = min(max(y, bl.y), bl.y+bl.n-1)
			ri := min(v.top+y-bl.y, len(v.rows)-1)
			l, c := v.rows[ri].line, v.colAt(ri, x)
			if v.mode == normal && (l != v.dl || c != v.dc) {
				v.al, v.ac, v.mode = v.dl, v.dc, visual
				v.shiftSel = true
			}
			v.cl, v.cc = l, c
			v.settle(v.dl, v.dc)
			v.want = v.vcol()
			return
		}
	case b&tcell.Button1 != 0:
		for _, bl := range v.blocks {
			if y < bl.y || y >= bl.y+bl.n {
				continue
			}
			if bl.lv == nil {
				if !v.descend() {
					return
				}
			}
			for bl.lv != nil && v.level != bl.lv {
				v.ascend()
			}
			ri := v.top + y - bl.y
			if ri >= len(v.rows) {
				return
			}
			if v.selecting() {
				v.mode = normal
			}
			v.cl, v.cc = v.rows[ri].line, v.colAt(ri, x)
			v.settle(v.cl, v.cc)
			v.want, v.msg, v.pend, v.count = v.vcol(), "", 0, 0
			v.drag, v.dl, v.dc = true, v.cl, v.cc
			return
		}
	default: // released
		v.drag = false
	}
}

// descend moves the cursor into the definitions pane, at the start of its
// first row in view; the pane becomes the text, with a pane of its own
// below it. It reports whether there was a pane of definitions to move
// into.
func (v *view) descend() bool {
	w, p := v.lookup()
	if v.help || v.ph == 0 || p == nil || p.Rows(w).Notice != "" {
		return false
	}
	pane := v.pane()
	if len(pane) == 0 {
		return false
	}
	name, src := v.d.name+" › "+w, v.d.src+" › "+w
	if len(v.stack) == 0 {
		name = fmt.Sprintf("%s:%d › %s", v.d.name, v.cl+1, w)
		src = fmt.Sprintf("%s:%d › %s", v.d.src, v.cl+1, w)
		v.bh = v.textH()
	}
	lv := &level{d: paneDoc(name, src, pane), top: v.pscroll,
		bh: min(v.ph, v.need(pane))}
	v.stack = append(v.stack, v.level)
	v.level = lv
	v.wrap()
	v.top = min(v.top, len(v.rows)-1)
	v.cl, v.cc = v.rows[v.top].line, v.rows[v.top].start
	v.settle(v.cl-1, 0)
	v.want = v.vcol()
	return true
}

// settle moves the cursor off what is not definition text in a pane moved
// into: to the nearest text on its line, looking first the way it moved
// from l0, c0, or when the line has none, to the nearest line that way
// that has some.
func (v *view) settle(l0, c0 int) {
	v.clamp()
	if !v.d.skipped(v.cl, v.cc) {
		return
	}
	x0 := v.vcol()
	step := -1
	if v.cl > l0 || v.cl == l0 && v.cc >= c0 {
		step = 1
	}
	for _, s := range []int{step, -step} {
		for l := v.cl; l >= 0 && l < len(v.d.lines); l += s {
			if c, ok := v.d.textNear(l, v.cc, s); ok {
				v.cl, v.cc = l, c
				if v.want == x0 {
					v.want = v.vcol()
				}
				return
			}
		}
	}
}

// ascend goes back to the level above, closing the pane moved into.
func (v *view) ascend() {
	if n := len(v.stack); n > 0 {
		v.level, v.stack = v.stack[n-1], v.stack[:n-1]
	}
}

// colAt is the character of display row ri at screen column x, or the
// row's last when x is past its end.
func (v *view) colAt(ri, x int) int {
	r := v.rows[ri]
	line := v.d.lines[r.line]
	c := r.start
	for cx := r.x; c+1 < r.end; c++ {
		cx += zhdict.Width(string(line[c]))
		if cx > x {
			break
		}
	}
	return c
}

func (v *view) runeKey(r rune) {
	if v.pend == 'g' {
		v.pend = 0
		v.gKey(r)
		return
	}
	if r >= '1' && r <= '9' || r == '0' && v.count > 0 {
		v.count = v.count*10 + int(r-'0')
		return
	}
	n, counted := max(v.count, 1), v.count > 0
	v.count = 0
	switch r {
	case 'h':
		v.cc = max(v.cc-n, 0)
		v.want = v.vcol()
	case 'l':
		v.cc += n
		v.clamp()
		v.want = v.vcol()
	case 'j':
		v.vert(n)
	case 'k':
		v.vert(-n)
	case 'w':
		for ; n > 0; n-- {
			v.wordFwd()
		}
		v.want = v.vcol()
	case 'b':
		for ; n > 0; n-- {
			v.wordBack()
		}
		v.want = v.vcol()
	case '0':
		v.cc = 0
		v.want = v.vcol()
	case '$':
		v.cc, v.want = v.lineLen(v.cl), 1<<30
	case 'G':
		v.cl = len(v.d.lines) - 1
		if counted {
			v.cl = n - 1
		}
		v.cc = 0
	case 'g':
		v.pend = 'g'
		v.count = n
		if !counted {
			v.count = 0
		}
	case '/', '?':
		v.openPrompt(r)
	case ':':
		if v.selecting() {
			v.openPrompt(':')
		}
	case 'J':
		v.pscroll += n
	case 'K':
		v.pscroll = max(v.pscroll-n, 0)
	case 'x':
		v.expand = !v.expand
	case 'c':
		v.moreZh = !v.moreZh
	case 'E':
		v.moreEn = !v.moreEn
	case 'n', 'N':
		for ; n > 0; n-- {
			v.search(v.back != (r == 'N'))
		}
	case '*', '#':
		if w, _ := v.lookupTok(); w != "" {
			v.pat = regexp.MustCompile(regexp.QuoteMeta(w))
			v.back = r == '#'
			v.search(v.back)
		}
	case 'v', 'V':
		m := visual
		if r == 'V' {
			m = vline
		}
		v.shiftSel = false
		if v.mode == m {
			v.mode = normal
		} else {
			if v.mode == normal {
				v.al, v.ac = v.cl, v.cc
			}
			v.mode = m
		}
	case 'm':
		if len(v.stack) > 0 {
			v.msg = "bookmarks are for the book"
		} else if err := v.d.setMark(v.cl); err != nil {
			v.msg = err.Error()
		} else {
			v.msg = fmt.Sprintf("bookmarked line %d", v.cl+1)
		}
	case '\'', '`':
		if len(v.stack) > 0 {
			v.msg = "bookmarks are for the book"
		} else if l := v.d.getMark(); l >= 0 {
			v.cl, v.cc = l, 0
		} else {
			v.msg = "no bookmark in this file"
		}
	case 'e':
		v.showEn = !v.showEn
	case 't':
		v.showTW = !v.showTW
	case 'q':
		if v.mode == normal {
			v.quit = true
		}
	}
}

func (v *view) toggleHelp() {
	v.help, v.pscroll = !v.help, 0
}

func (v *view) gKey(r rune) {
	n, counted := max(v.count, 1), v.count > 0
	v.count = 0
	row := v.rows[v.rowOf(v.cl, v.cc)]
	switch r {
	case 'h':
		v.toggleHelp()
	case 'g':
		v.cl, v.cc = 0, 0
		if counted {
			v.cl = n - 1
		}
	case 'j':
		v.byRows(n)
	case 'k':
		v.byRows(-n)
	case '0':
		v.cc = row.start
		v.want = v.vcol()
	case '$':
		v.cc = max(row.end-1, row.start)
		v.want = v.vcol()
	}
}

// vcol is the cursor's display column in its line, as if the line did not
// wrap, from the column a pane's line starts at: what j and k keep, so
// that they go straight down the screen across lines that start at
// different columns, as a pane's do.
func (v *view) vcol() int {
	x := 0
	if v.d.ind != nil {
		x = v.d.ind[v.cl][0]
	}
	line := v.d.lines[v.cl]
	return x + zhdict.Width(string(line[:min(v.cc, len(line))]))
}

// vert moves the cursor n lines, to the character at display column want.
func (v *view) vert(n int) {
	v.cl = min(max(v.cl+n, 0), len(v.d.lines)-1)
	x := 0
	if v.d.ind != nil {
		x = v.d.ind[v.cl][0]
	}
	line := v.d.lines[v.cl]
	v.cc = 0
	for c, r := range line {
		if x > v.want {
			break
		}
		v.cc = c
		x += zhdict.Width(string(r))
	}
}

// byRows moves the cursor n display rows, keeping its screen column.
func (v *view) byRows(n int) {
	ri := v.rowOf(v.cl, v.cc)
	row := v.rows[ri]
	x := row.x + zhdict.Width(string(v.d.lines[v.cl][row.start:v.cc]))
	ri = min(max(ri+n, 0), len(v.rows)-1)
	v.cl, v.cc = v.rows[ri].line, v.colAt(ri, x)
}

// wordFwd and wordBack move to the next and previous word, passing over
// punctuation.
func (v *view) wordFwd() {
	l, c := v.cl, v.cc
	for {
		for _, t := range v.d.lineToks(l) {
			if t.start > c && !v.d.punct(l, t) &&
				!v.d.skipped(l, t.start) {
				v.cl, v.cc = l, t.start
				return
			}
		}
		if l+1 >= len(v.d.lines) {
			return
		}
		l, c = l+1, -1
	}
}

func (v *view) wordBack() {
	l, c := v.cl, v.cc
	for {
		ts := v.d.lineToks(l)
		for i := len(ts) - 1; i >= 0; i-- {
			if ts[i].start < c && !v.d.punct(l, ts[i]) &&
				!v.d.skipped(l, ts[i].start) {
				v.cl, v.cc = l, ts[i].start
				return
			}
		}
		if l == 0 {
			return
		}
		l--
		c = v.lineLen(l) + 1
	}
}

// lookupTok is the word under the cursor, of any kind.
func (v *view) lookupTok() (string, tok) {
	for _, t := range v.d.lineToks(v.cl) {
		if t.start <= v.cc && v.cc < t.end {
			return string(v.d.lines[v.cl][t.start:t.end]), t
		}
	}
	return "", tok{}
}

func (v *view) openPrompt(c rune) {
	v.pfrom, v.mode, v.pchar, v.input = v.mode, prompt, c, nil
}

// closePrompt returns to the mode the prompt was opened from: a search
// in visual mode moves the end of the selection, as in vi. A command
// ends the selection.
func (v *view) closePrompt() {
	v.mode = v.pfrom
	if v.pchar == ':' {
		v.mode = normal
	}
}

func (v *view) promptKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEscape:
		v.closePrompt()
	case tcell.KeyEnter:
		if v.pchar == ':' {
			v.command(string(v.input))
			v.closePrompt()
			return
		}
		v.closePrompt()
		if len(v.input) == 0 && v.pat != nil {
			v.back = v.pchar == '?'
			v.search(v.back)
			return
		}
		re, err := regexp.Compile(string(v.input))
		if err != nil {
			v.msg = err.Error()
			return
		}
		v.pat, v.back = re, v.pchar == '?'
		v.search(v.back)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(v.input) > 0 {
			v.input = v.input[:len(v.input)-1]
		} else {
			v.closePrompt()
		}
	case tcell.KeyRune:
		v.input = append(v.input, ev.Rune())
	}
}

// command runs a command typed at the : prompt. The one command is
// >cmd, which pipes the selection to cmd.
func (v *view) command(c string) {
	c = strings.TrimSpace(c)
	switch {
	case c == "":
	case strings.HasPrefix(c, ">"):
		if c = strings.TrimSpace(c[1:]); c == "" {
			v.msg = "no command after >"
			return
		}
		v.pipe(c, v.selText())
	default:
		v.msg = "not a command: " + c
	}
}

// selText is the selected text, its lines joined by newlines.
func (v *view) selText() string {
	l1, c1, l2, c2 := v.selRange()
	var sb strings.Builder
	for l := l1; l <= l2; l++ {
		line := v.d.lines[l]
		a, b := 0, len(line)
		if l == l1 {
			a = min(c1, b)
		}
		if l == l2 {
			b = min(c2+1, b)
		}
		sb.WriteString(string(line[a:b]))
		if l < l2 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// pipeDone reports a piped command's failure to the event loop.
type pipeDone string

// pipe starts cmd with sh, the text on its standard input, and returns
// without waiting for it: the commands piped to open windows of their
// own. It runs in a session of its own, so quitting zhread, and closing
// its window, does not take the command with it. Its output is
// discarded but for the end of what it writes to stderr, shown if it
// fails.
func (v *view) pipe(cmd, text string) {
	c := exec.Command("sh", "-c", cmd)
	c.Stdin = strings.NewReader(text)
	var errb tailBuf
	c.Stderr = &errb
	detach(c)
	if err := c.Start(); err != nil {
		v.msg = err.Error()
		return
	}
	go func() {
		if err := c.Wait(); err != nil {
			msg := cmd + ": " + err.Error()
			if e := strings.TrimSpace(string(errb)); e != "" {
				msg += ": " + e
			}
			v.s.PostEvent(tcell.NewEventInterrupt(pipeDone(msg)))
		}
	}()
	v.msg = "piped the selection to " + cmd
}

// tailBuf keeps the last 200 bytes written to it.
type tailBuf []byte

func (b *tailBuf) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	if len(*b) > 200 {
		*b = (*b)[len(*b)-200:]
	}
	return len(p), nil
}

// search moves to the next match of the pattern, wrapping around the file.
func (v *view) search(back bool) {
	if v.pat == nil {
		v.msg = "no previous pattern"
		return
	}
	n := len(v.d.lines)
	l := v.cl
	for i := 0; i <= n; i++ {
		s := string(v.d.lines[l])
		var hit = -1
		for _, m := range v.pat.FindAllStringIndex(s, -1) {
			c := utf8.RuneCountInString(s[:m[0]])
			before := !back && c <= v.cc || back && c >= v.cc
			if i == 0 && before {
				continue
			}
			if hit < 0 || back {
				hit = c
			}
			if !back {
				break
			}
		}
		if hit >= 0 {
			v.cl, v.cc = l, hit
			v.want = v.vcol()
			return
		}
		if back {
			l = (l - 1 + n) % n
		} else {
			l = (l + 1) % n
		}
	}
	v.msg = "pattern not found: " + v.pat.String()
}

// openCard starts a card for the selection, or in normal mode for the
// word under the cursor. When the words can be divided more than one way,
// as 牟取暴利 is one word to one dictionary and 牟取 and 暴利 to another, it
// asks which first.
func (v *view) openCard() {
	var l1, c1, l2, c2 int
	switch {
	case v.selecting():
		l1, c1, l2, c2 = v.selRange()
	case v.mode == normal:
		_, t := v.lookupTok()
		if !t.han {
			return
		}
		l1, c1, l2, c2 = v.cl, t.start, v.cl, t.end-1
	default:
		return
	}
	var p para
	if v.d.sty != nil {
		// A pane's context is the definition the word is in.
		if l2 != l1 {
			v.msg = "the selection crosses a line of the " +
				"definitions"
			return
		}
		p = v.d.paneLine(l1)
	} else {
		p = v.d.paragraph(l1)
		if l2 >= p.first+len(p.offs) {
			v.msg = "the selection crosses a paragraph break"
			return
		}
	}
	clip := func(i int) int { return min(max(i, 0), len(p.text)) }
	ws, we := clip(p.pos(l1, c1)), clip(p.pos(l2, c2)+1)
	for ws < we && strings.TrimSpace(string(p.text[ws])) == "" {
		ws++
	}
	for we > ws && strings.TrimSpace(string(p.text[we-1])) == "" {
		we--
	}
	if ws == we {
		return
	}
	if err := v.loadConvs(); err != nil {
		v.msg = err.Error()
		return
	}
	// The text is traditional if converting it to simplified changes
	// more of it than converting it to traditional does.
	text := string(p.text)
	trad := changed(text, v.conv.t2s.Convert(text)) >
		changed(text, v.conv.s2t.Convert(text))
	word := string(p.text[ws:we])
	pr := zhdict.NewProbe(zhdict.HanRuns(word))
	pr.Load(v.dict)
	pr.LoadExtra(v.dict) // checked at startup
	pr.LoadPron(v.dict)
	pr.LoadTargets(v.dict)
	pr.PronPref, pr.Trad = v.pron, trad
	src := fmt.Sprintf("%s:%d", v.d.src, l1+1)
	if len(v.stack) > 0 {
		src = v.d.src // /path/to/book:line › word
	}
	pk := &picker{p: p, ws: ws, we: we, pr: pr, src: src, trad: trad,
		opts: segOptions(pr, p.text, ws, we, v.dict)}
	switch len(pk.opts) {
	case 0:
		v.msg = "no definitions for any word in " + word
	case 1:
		v.startCards(pk, pk.opts[0])
	default:
		v.pick, v.mode = pk, choose
	}
}

// picker is a selection being made into cards: the ways to divide it into
// words, for choose to pick from.
type picker struct {
	p      para
	ws, we int // the selection, as offsets into p.text
	pr     *zhdict.Probe
	src    string
	trad   bool
	opts   [][][2]int // each a list of words, as offsets into p.text
	cur    int
}

// maxSplits bounds the other divisions of a word offered, and splitRunes
// the length of word they are looked for in.
const (
	maxSplits  = 6
	splitRunes = 12
)

// segOptions are the ways to divide text[ws:we] into words to card. The
// first is the selection as one word, if it is one, or else the words of it
// the segmenter finds that have definitions, which is what a selection no
// dictionary has as one word, a phrase, is carded as. The rest are the other
// ways to divide it wholly into words of two characters or more, as 牟取暴利
// into 牟取 and 暴利, and for a phrase what the two maximum-matching
// segmenters make of it. Words of one character are left out of the
// divisions of a word, which would otherwise offer 散 and 发 for every 散发.
func segOptions(pr *zhdict.Probe, text []rune, ws, we int,
	d *zhdict.Dict) [][][2]int {
	var out [][][2]int
	add := func(o [][2]int) {
		if len(o) == 0 || slices.ContainsFunc(out,
			func(x [][2]int) bool { return slices.Equal(x, o) }) {
			return
		}
		out = append(out, o)
	}
	word := string(text[ws:we])
	segWords := func(mode string) [][2]int {
		seg := func(run []rune) []string {
			return pr.Segment([][]rune{run}, mode)
		}
		var o [][2]int
		for _, t := range tokenize(text[ws:we], seg) {
			w := string(text[ws+t.start : ws+t.end])
			if t.han && pr.Has(w) {
				o = append(o, [2]int{ws + t.start, ws + t.end})
			}
		}
		return o
	}
	if pr.Has(word) {
		add([][2]int{{ws, we}})
	} else {
		mode := zhdict.SegViterbi
		if pr.LoadFreq(d) != nil {
			mode = zhdict.SegBidi
		}
		add(segWords(mode))
		add(segWords(zhdict.SegFMM))
		add(segWords(zhdict.SegBMM))
	}
	runs := zhdict.HanRuns(word)
	if len(runs) != 1 || string(runs[0]) != word || we-ws > splitRunes {
		return out
	}
	var split func(i int, cur [][2]int)
	split = func(i int, cur [][2]int) {
		if len(out) > maxSplits {
			return
		}
		if i == we {
			add(slices.Clone(cur))
			return
		}
		for j := we; j >= i+2; j-- {
			if i == ws && j == we || !pr.Has(string(text[i:j])) {
				continue
			}
			split(j, append(cur, [2]int{i, j}))
		}
	}
	split(ws, nil)
	return out
}

// startCards queues a card for each of the words, in turn.
func (v *view) startCards(pk *picker, words [][2]int) {
	v.queue, v.pick = nil, nil
	for i, sp := range words {
		ws, we := sp[0], sp[1]
		dr := &draft{p: pk.p, ws: ws, we: we,
			span: newSpan(pk.p.text, ws, we),
			src:  pk.src, trad: pk.trad, n: i + 1, of: len(words),
			g: pk.pr.Rows(string(pk.p.text[ws:we]))}
		dr.notice = dr.g.Notice
		dr.setOpts(v.shows)
		if dr.zh > 0 {
			dr.sel[0] = true
		}
		if len(dr.opts) > dr.zh {
			dr.sel[dr.zh] = true
		}
		v.queue = append(v.queue, dr)
	}
	v.nextCard()
}

func (v *view) chooseKey(ev *tcell.EventKey) {
	pk := v.pick
	r := ev.Rune()
	switch {
	case ev.Key() == tcell.KeyEscape || r == 'q':
		v.mode, v.pick = normal, nil
	case ev.Key() == tcell.KeyEnter || r == ' ':
		v.startCards(pk, pk.opts[pk.cur])
	case r >= '1' && r <= '9' && int(r-'1') < len(pk.opts):
		v.startCards(pk, pk.opts[r-'1'])
	case r == 'j' || ev.Key() == tcell.KeyDown:
		pk.cur = min(pk.cur+1, len(pk.opts)-1)
	case r == 'k' || ev.Key() == tcell.KeyUp:
		pk.cur = max(pk.cur-1, 0)
	}
}

// drawChoose shows the ways to divide the selection, one a line, with the
// words of each separated by a dot.
func (v *view) drawChoose() {
	pk := v.pick
	maxY := v.h - 1
	whole := string(pk.p.text[pk.ws:pk.we])
	y := v.put(0, 0, maxY, []rune(" Divide "+whole+" into:"),
		plain(stHead)) + 1
	for i, o := range pk.opts {
		var ws []string
		for _, w := range o {
			ws = append(ws, string(pk.p.text[w[0]:w[1]]))
		}
		st := stDef
		if i == pk.cur {
			st = stSel
		}
		y = v.put(0, y, maxY, []rune(fmt.Sprintf(" %d  %s", i+1,
			strings.Join(ws, " · "))), plain(st))
	}
	help := "j k: choose  Enter or 1-9: make the cards  Esc: cancel"
	v.put(0, v.h-1, v.h, clip([]rune(help), v.w), plain(stHint))
}

// nextCard shows the next card of the selection, or returns to reading
// when there are none left. The duplicate check runs only now, so a word
// the selection repeats is caught against the card just saved for it.
func (v *view) nextCard() {
	if len(v.queue) == 0 {
		v.mode, v.dr = normal, nil
		return
	}
	v.dr, v.queue = v.queue[0], v.queue[1:]
	dr := v.dr
	f := dr.forms(v.conv)
	dr.dups = v.rec.dups(f.wordS, f.wordT)
	dr.exact = exact(dr.dups, f)
	// For a word already carded, the senses ticked to start with are
	// those on its cards, where any are shown: what u would add to is
	// then what is ticked, and a new card for another sense is a matter
	// of ticking that one instead.
	if len(dr.exact) > 0 {
		var on []bool
		for _, o := range dr.opts {
			on = append(on, v.rec.cardedSense(f.wordS, o.s) ||
				v.rec.cardedSense(f.wordT, o.s))
		}
		if slices.Contains(on, true) {
			dr.sel = on
		}
	}
	v.mode = card
}

// edit opens the existing card k in the preview, in place of the new card
// for its word: see editCard.
func (v *view) edit(k carded) {
	if err := v.editCard(v.dr, k); err != nil {
		v.dr.warn = err.Error()
	}
}

func changed(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for i := range min(len(ra), len(rb)) {
		if ra[i] != rb[i] {
			n++
		}
	}
	return n
}

func (v *view) loadConvs() error {
	if v.conv.s2t != nil {
		return nil
	}
	s2t, err := opencc.Load(v.convDir, "s2tw")
	if err != nil {
		return err
	}
	t2s, err := opencc.Load(v.convDir, "tw2s")
	if err != nil {
		return err
	}
	v.conv = convs{s2t, t2s}
	return nil
}

// ctxLines are the scripts of the context lines the preview shows, top
// first, as indices into draft.ed: both when the word differs between
// them, the one case the traditional cards are made, else the text's own.
func (dr *draft) ctxLines(f forms) []int {
	switch {
	case f.wordT != f.wordS:
		return []int{0, 1}
	case dr.trad:
		return []int{1}
	}
	return []int{0}
}

func (v *view) cardKey(ev *tcell.EventKey) {
	dr := v.dr
	r := ev.Rune()
	if ev.Key() != tcell.KeyRune {
		r = 0
	}
	f := dr.forms(v.conv)
	lines := dr.ctxLines(f)
	dr.cur = max(dr.cur, -len(lines))
	// An Esc right after another, as from insert mode back out to the
	// list and on, asks before it throws away edits to the contexts.
	esc := ev.Key() == tcell.KeyEscape
	again := esc && dr.esc
	dr.esc = esc
	if dr.discard {
		dr.discard = false
		if r == 'y' {
			v.mode, v.dr, v.queue = normal, nil, nil
		}
		return
	}
	// The keys: F1 shows or hides them anywhere, gh too but where g is
	// text or a d, c or r's motion, and Esc hides them in the list, where
	// it would otherwise cancel the card.
	cv := dr.curVi()
	free := !dr.editing || !cv.insert && cv.pend == 0
	g := dr.gPend
	dr.gPend = false
	switch {
	case ev.Key() == tcell.KeyF1 || g && r == 'h':
		v.chelp, v.cscroll, dr.esc = !v.chelp, 0, false
		return
	case g: // g and any other key does nothing
		return
	case r == 'g' && free:
		dr.gPend = true
		return
	case esc && v.chelp && !dr.editing:
		v.chelp, dr.esc = false, false
		return
	case v.chelp && !dr.editing && r == 'J':
		v.cscroll++
		return
	case v.chelp && !dr.editing && r == 'K':
		v.cscroll--
		return
	}
	o := dr.other()
	// A context, the card's own or another, is in the list until i
	// starts the editor on it in normal mode, and is back in it after Esc
	// there, or j or k.
	if dr.editing {
		ed, ef, k := dr, f, 0
		if dr.cur < 0 {
			k = lines[len(lines)+dr.cur]
		} else {
			ed, ef, k = o, o.ctxf, o.own()
		}
		if ev.Key() == tcell.KeyEscape && !ed.vi.insert {
			dr.editing = false
			return
		}
		if ed.edit(ev, ef, k, func() { dr.saving = false }) {
			if ed != dr {
				ed.refresh(v.conv)
			}
			dr.warn = ""
			return
		}
		if r == 'j' || r == 'k' || ev.Key() == tcell.KeyDown ||
			ev.Key() == tcell.KeyUp {
			dr.editing = false
		}
	}
	dr.warn = ""
	if dr.which {
		dr.which = false
		if r >= '1' && int(r-'1') < len(dr.exact) {
			v.edit(dr.exact[r-'1'])
		}
		return
	}
	switch {
	case r == 'u' && len(dr.exact) > 0:
		if len(dr.exact) == 1 {
			v.edit(dr.exact[0])
		} else {
			dr.which = true
		}
	case esc && again && dr.edited():
		dr.discard = true
	case esc || r == 'q':
		v.mode, v.dr, v.queue = normal, nil, nil
	case r == 'n':
		v.nextCard()
	case ev.Key() == tcell.KeyEnter || r == 'y':
		if f.wsS == f.weS || f.wsT == f.weT {
			dr.warn = "the context no longer has the word; " +
				"u undoes"
			return
		}
		if len(dr.dups) > 0 && !dr.saving {
			dr.saving = true
			return
		}
		note := dr.note(f, v.showTW, v.expand)
		var err error
		if dr.guid != "" {
			// over the card being edited, as it was first made
			note[0], note[11] = dr.guid, dr.added
			err = v.rec.update(dr.guid, func(row []string) {
				copy(row, note)
			})
		} else {
			err = v.rec.save(note, v.deck)
		}
		switch {
		case err != nil:
			v.msg = err.Error()
		case dr.guid != "":
			v.msg = "updated the card for " +
				headword(f.wordT, f.wordS)
		default:
			v.msg = "saved a card for " +
				headword(f.wordT, f.wordS)
		}
		v.nextCard()
	case strings.ContainsRune("[]{}", r) && r != 0 && o != nil:
		o.span.move(r)
		dr.changed = true
		o.ed, o.vi = [2]*edited{}, vi{}
		o.refresh(v.conv)
	case strings.ContainsRune("[]{}", r) && r != 0:
		// The edges move over the text as it was, so they undo edits.
		dr.span.move(r)
		dr.changed = true
		dr.ed, dr.vi = [2]*edited{}, vi{}
		dr.saving = false
	case r == 'i' && (dr.cur < 0 || o != nil):
		dr.editing = true
		if o != nil {
			o.vi.insert = false
		} else {
			dr.vi.insert = false
		}
	case (r == '<' || r == '>') && o != nil:
		dr.moveOther(r == '>')
	case ev.Key() == tcell.KeyPgDn:
		dr.cur = max(min(dr.cur+10, dr.rows()-1), dr.cur)
	case ev.Key() == tcell.KeyPgUp:
		dr.cur = max(dr.cur-10, -len(lines))
	case r == 'j' || ev.Key() == tcell.KeyDown:
		dr.cur = max(min(dr.cur+1, dr.rows()-1), dr.cur)
	case r == 'k' || ev.Key() == tcell.KeyUp:
		dr.cur = max(dr.cur-1, -len(lines))
	case r == ' ' && dr.cur >= 0 && dr.cur < dr.rows():
		dr.tick(dr.cur)
		dr.changed = true
	case r == 'x':
		v.expand = !v.expand
	case r == 'c' || r == 'E':
		if r == 'c' {
			v.moreZh = !v.moreZh
		} else {
			v.moreEn = !v.moreEn
		}
		dr.setOpts(v.shows)
	}
}

// edit gives a key to the editor on context line k of the draft, whose
// forms are f, and reports whether it took it. The first change makes the
// line an edited copy of its own; changed is called on every change.
func (dr *draft) edit(ev *tcell.EventKey, f forms, k int,
	changed func()) bool {
	e := dr.ed[k]
	if e == nil {
		ctx, ws, we := f.ctxS, f.wsS, f.weS
		if k == 1 {
			ctx, ws, we = f.ctxT, f.wsT, f.weT
		}
		e = &edited{append([]rune(nil), ctx...), ws, we}
	}
	return dr.vi.key(ev, e, func() {
		dr.vi.undo = append(dr.vi.undo, e.clone())
		dr.ed[k] = e
		changed()
	})
}

func (v *view) drawCard() {
	dr := v.dr
	if dr.cur >= 0 && dr.other() == nil {
		dr.editing = false
	}
	f := dr.forms(v.conv)
	maxY := v.h - 1
	// The keys, on gh, in a pane at the bottom that leaves the card
	// eight rows, for its word, contexts and a few options, or half the
	// screen if that is more.
	var keys []styled
	ph := 0
	if v.chelp && v.h >= 6 {
		keys = keyLines(cardKeyHelp)
		ph = min(v.need(keys), max(v.h-9, v.h/2))
		maxY -= ph
	}
	head := " New card: " + headword(f.wordT, f.wordS)
	if dr.guid != "" {
		head = " Edit card: " + headword(f.wordT, f.wordS)
	}
	if dr.of > 1 {
		head += fmt.Sprintf("  (%d of %d)", dr.n, dr.of)
	}
	y := v.put(0, 0, maxY, []rune(head), plain(stHead)) + 1
	lines := dr.ctxLines(f)
	dr.cur = max(dr.cur, -len(lines))
	for n, k := range lines {
		label, rs, ws, we := "简", f.ctxS, f.wsS, f.weS
		if k == 1 {
			label, rs, ws, we = "繁", f.ctxT, f.wsT, f.weT
		}
		word := wordAt(rs, ws, we)
		lr := []rune(label + "  ")
		cur := -1
		on := dr.cur == n-len(lines)
		if on && dr.editing {
			dr.vi.col = min(dr.vi.col, len(rs))
			cur = len(lr) + dr.vi.col
			if dr.vi.col == len(rs) {
				rs = append(rs[:len(rs):len(rs)], ' ')
			}
		}
		style := func(i int) tcell.Style {
			switch {
			case i == cur:
				return stCursor
			case i < len(lr):
				return stHint
			case on && !dr.editing:
				return stSel
			case i >= len(lr) && i-len(lr) < len(word) &&
				word[i-len(lr)]:
				return stWord
			}
			return stDef
		}
		y = v.put(0, y, maxY, append(lr, rs...), style)
	}
	y++
	if len(dr.opts) == 0 {
		y = v.put(0, y, maxY, []rune(dr.notice), plain(stWarn))
	}
	// The options and other contexts scroll to keep the one under the
	// cursor in view, above the cards already made and the help line.
	end := maxY
	if len(dr.dups) > 0 {
		end = max(maxY-len(dr.dups)-2, y+1)
	}
	rows := func(i int) int {
		n := 0
		for _, l := range v.optLines(i) {
			n += v.height(l.x, l.wx, l.text)
		}
		return n
	}
	cur := max(dr.cur, 0)
	dr.top = min(dr.top, cur)
	// The first in view is the one furthest up, from the cursor's, that
	// lets the rows down to the cursor's fit, with a row for the note of
	// options scrolled past, once there are any. It is worked out back
	// from the cursor, so that only what fits is measured, however many
	// contexts the card has.
	if dr.top < cur {
		t, n := cur, rows(cur)
		for t > dr.top {
			h := rows(t - 1)
			if y+1+n+h > end {
				break
			}
			t, n = t-1, n+h
		}
		dr.top = t
	}
	if dr.top > 0 {
		y = v.put(0, y, end, []rune(fmt.Sprintf("  ↑ %d more",
			dr.top)), plain(stHint))
	}
	for i := dr.top; i < dr.rows() && y < end; i++ {
		for _, l := range v.optLines(i) {
			y = v.putw(l.x, l.wx, y, 0, end, l.text, l.style)
		}
	}
	if len(dr.dups) > 0 {
		y++
		y = v.put(0, y, maxY, []rune(fmt.Sprintf(
			"Already carded (%d):", len(dr.dups))), plain(stWarn))
		// The cards u can add to are numbered, for when it asks which.
		n := 0
		for _, k := range dr.dups {
			w := k.simp
			if k.trad != "" {
				w = headword(k.trad, k.simp)
			}
			line := "   " + w + "  " + k.py + "  "
			isK := func(e carded) bool { return e.guid == k.guid }
			if slices.ContainsFunc(dr.exact, isK) {
				n++
				line = fmt.Sprintf("%d  %s", n, line[3:])
			}
			if k.ctx != "" {
				line += k.ctx
			} else {
				line += k.def
			}
			line += "  (" + filepath.Base(k.src) + ")"
			y = v.put(0, y, maxY, clip([]rune(line), v.w),
				plain(stDef))
		}
	}
	if ph > 0 {
		v.drawPane(maxY, ph, &v.cscroll, keys, "")
	}
	// The keys are listed on gh, not here, but for the questions.
	help := ""
	st := stHint
	switch {
	case dr.discard:
		help, st = "Throw away the changes to this card? y: yes  "+
			"any other key: no", stWarn
	case dr.warn != "":
		help, st = dr.warn, stWarn
	case dr.which:
		help, st = fmt.Sprintf("Add to which card? 1-%d  "+
			"any other key: none", len(dr.exact)), stWarn
	case dr.saving && len(dr.exact) > 0:
		help, st = "Already carded. Enter: new card  u: edit it"+
			"  Esc: cancel", stWarn
	case dr.saving:
		help, st = "Already carded. Enter: save anyway  Esc: cancel",
			stWarn
	}
	// The mode, first: the list of definitions and contexts, or the
	// editor on a context, in normal or insert mode.
	mode := "-- LIST --"
	switch {
	case dr.editing && dr.curVi().insert:
		mode = "-- INSERT --"
	case dr.editing:
		mode = "-- NORMAL --"
	}
	// At the right, the toggles that would change the card, as on the
	// reader's rule.
	right := ""
	if m := v.cardToggles(); m != "" {
		right = " " + m + " "
	}
	text := clip([]rune(mode+"  "+help), v.w-zhdict.Width(right))
	n := len([]rune(mode))
	v.put(0, v.h-1, v.h, text, func(i int) tcell.Style {
		if i < n {
			return stHead
		}
		return st
	})
	if right != "" {
		v.put(max(v.w-zhdict.Width(right), 0), v.h-1, v.h,
			[]rune(right), plain(stHint))
	}
}

// optLines are the lines option i takes in the card preview: a blank one
// before the first English option, the option itself, and the quotations
// that go on the card with it after x. Past the options, they are the
// other contexts'.
func (v *view) optLines(i int) []styled {
	dr := v.dr
	if i >= len(dr.opts) {
		return v.otherLines(i - len(dr.opts))
	}
	o := dr.opts[i]
	var out []styled
	if i == dr.zh && i > 0 {
		out = append(out, line(0))
	}
	box := "[ ] "
	if dr.sel[i] {
		box = "[x] "
	}
	ps := []piece{{box, stDef}, {v.reading(o.py) + "  ", stPinyin}}
	if o.src != "" {
		ps = append(ps, piece{o.src + "  ", stHint})
	}
	if o.s.Pos != "" {
		ps = append(ps, piece{o.s.Pos + " ", stPos})
	}
	for _, h := range o.s.Heads {
		ps = append(ps, labelled(h)...)
		ps = append(ps, piece{" ", stDef})
	}
	ps = append(ps, sensePieces(o.s)...)
	l := line(0, ps...)
	l.wx = 4
	if i == dr.cur {
		l.style = plain(stSel)
	} else if !dr.sel[i] {
		l.style = plain(stHint)
		// one row each for the rest, so the list stays short
		l.text = clip(l.text, v.w)
	}
	out = append(out, l)
	if dr.sel[i] && v.expand {
		for _, c := range o.s.Cites {
			out = append(out, line(6, piece{c, stHint}))
		}
	}
	return out
}

// doubled widens the word at [ws, we) of t to the doubled form of it that
// it is part of, if any: AABB or ABAB for a word AB, as 松松垮垮 for 松垮
// and 研究研究 for 研究, and AA, A一A or A了A for a word A, as 看看, 看一看
// and 看了看 for 看.
func doubled(t []rune, ws, we int) (int, int) {
	// has reports whether t has rs at i.
	has := func(i int, rs ...rune) bool {
		return i >= 0 && i+len(rs) <= len(t) &&
			slices.Equal(t[i:i+len(rs)], rs)
	}
	w := t[ws:we]
	switch len(w) {
	case 1:
		a := w[0]
		for _, mid := range []string{"", "一", "了"} {
			m := []rune(mid)
			n := len(m) + 1
			if has(we, append(m, a)...) {
				return ws, we + n
			}
			if has(ws-n, append([]rune{a}, m...)...) {
				return ws - n, we
			}
		}
	case 2:
		a, b := w[0], w[1]
		switch {
		case has(ws-1, a) && has(we, b):
			return ws - 1, we + 1
		case has(we, a, b):
			return ws, we + 2
		case has(ws-2, a, b):
			return ws - 2, we
		}
	}
	return ws, we
}

// wordAt marks the characters of rs the card shows as its word: the
// word at ws, we, or the doubled form around it, and every other place rs
// has the word, as mark marks them on the card.
func wordAt(rs []rune, ws, we int) []bool {
	on := make([]bool, len(rs))
	word := rs[ws:we]
	for i := 0; len(word) > 0 && i+len(word) <= len(rs); i++ {
		if slices.Equal(rs[i:i+len(word)], word) {
			for k := i; k < i+len(word); k++ {
				on[k] = true
			}
		}
	}
	ws, we = doubled(rs, ws, we)
	for k := ws; k < we; k++ {
		on[k] = true
	}
	return on
}

// otherLines are the lines other context k takes in the card preview: a
// heading before the first, and the context in the text's script, with
// the word in its colour, and the cursor in it while it is edited.
func (v *view) otherLines(k int) []styled {
	dr, o := v.dr, v.dr.others[k]
	f := o.ctxf
	rs, ws, we := f.ctxS, f.wsS, f.weS
	if dr.trad {
		rs, ws, we = f.ctxT, f.wsT, f.weT
	}
	var out []styled
	if k == 0 {
		out = append(out, line(0), line(0, piece{
			"Other contexts, in order (< > move one):", stHint}))
	}
	box := "[ ] "
	if dr.osel[k] {
		box = "[x] "
	}
	ps := []piece{{box, stDef}}
	on := wordAt(rs, ws, we)
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && on[j] == on[i] {
			j++
		}
		st := stDef
		if on[i] {
			st = stWord
		}
		ps = append(ps, piece{string(rs[i:j]), st})
		i = j
	}
	if o.from != "" {
		ps = append(ps, piece{"  " + o.from, stHint})
	}
	l := line(0, ps...)
	l.wx = 4
	if len(dr.opts)+k == dr.cur && dr.editing {
		o.vi.col = min(o.vi.col, len(rs))
		cur := len([]rune(box)) + o.vi.col
		if o.vi.col == len(rs) {
			l.text = append(l.text, ' ')
		}
		st := l.style
		l.style = func(i int) tcell.Style {
			if i == cur {
				return stCursor
			}
			return st(i)
		}
	} else if len(dr.opts)+k == dr.cur {
		l.style = plain(stSel)
	} else if !dr.osel[k] {
		l.style = plain(stHint)
		l.text = clip(l.text, v.w)
	}
	return append(out, l)
}

// clip cuts text to fit w columns, marking the cut with an ellipsis.
func clip(text []rune, w int) []rune {
	x := 0
	for i, r := range text {
		x += zhdict.Width(string(r))
		if x > w-1 {
			return append(text[:i:i], '…')
		}
	}
	return text
}
