package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
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
)

// drow is one display row: a slice of a line that fits the screen width.
type drow struct{ line, start, end int }

type view struct {
	s       tcell.Screen
	d       *doc
	dict    *zhdict.Dict
	rec     *record
	reqs    chan<- segReq
	conv    convs
	convDir string
	deck    string
	pron    string

	w, h     int
	ph       int // rows of the definitions pane, rule included
	rows     []drow
	firstRow []int // index into rows of each line's first row
	top      int   // first row on screen

	cl, cc  int // cursor line and column
	want    int // column j and k aim for
	al, ac  int // visual anchor
	mode    int
	pfrom   int  // the mode the prompt was opened from
	pchar   rune // the prompt's kind: / to search, : for a command
	pend    rune // first key of a two-key command (g)
	count   int
	showEn  bool
	showTW  bool   // also show the other reading: TW, or CN with -pron tw
	help    bool   // the pane shows the keys instead of definitions
	expand  bool   // show MoEDict's quotations, in the pane and on cards
	pword   string // the word the pane was last scrolled for
	pscroll int    // rows of the pane scrolled past, J and K
	moreZh  bool   // show MoEDict where 兩岸詞典 defines the word too, C
	moreEn  bool   // show Wiktionary where CC-CEDICT defines it too, E
	msg     string

	pat   *regexp.Regexp
	back  bool // last search went backward
	input []rune
	dr    *draft
	queue []*draft // the selection's cards still to come after dr
	quit  bool
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
				v.d.apply(d)
			case pipeDone:
				v.msg = string(d)
			}
		case *tcell.EventKey:
			v.key(ev)
		}
	}
	return nil
}

// layout: the text above, the definitions pane below it, a status line.
// The pane is as tall as the current definitions need, up to half the
// screen; draw sets ph from them before laying out the text.
func (v *view) textH() int { return v.h - 1 - v.ph }

// wrap splits every line into display rows for the current width.
func (v *view) wrap() {
	w, h := v.s.Size()
	if v.rows != nil && w == v.w && h == v.h {
		return
	}
	v.w, v.h = w, h
	v.rows = v.rows[:0]
	v.firstRow = make([]int, len(v.d.lines)+1)
	for l, line := range v.d.lines {
		v.firstRow[l] = len(v.rows)
		start, x := 0, 0
		for c, r := range line {
			rw := zhdict.Width(string(r))
			if x+rw > w && c > start {
				v.rows = append(v.rows, drow{l, start, c})
				start, x = c, 0
			}
			x += rw
		}
		v.rows = append(v.rows, drow{l, start, len(line)})
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
func (v *view) scroll() {
	v.top = max(v.rowOf(v.cl, v.cc)-v.anchor(), 0)
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
	var rq segReq
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
	v.wrap()
	v.clamp()
	v.s.Clear()
	if v.mode == card {
		v.drawCard()
		v.s.Show()
		return
	}
	if w, _ := v.lookup(); w != v.pword {
		v.pword, v.pscroll = w, 0
	}
	pane := v.pane()
	v.ph = 0
	if v.h >= 6 {
		need := 1
		for _, t := range pane {
			need += v.height(t.x, t.wx, t.text)
		}
		// The keys may take all but a few rows of text; definitions,
		// half the screen.
		top := v.h / 2
		if v.help {
			top = v.h - 3
		}
		v.ph = min(need, top)
	}
	v.scroll()
	th := v.textH()
	for y := 0; y < th && v.top+y < len(v.rows); y++ {
		v.drawRow(y, v.rows[v.top+y])
	}
	if v.ph > 0 {
		v.drawPane(th, pane)
	}
	v.s.HideCursor()
	v.drawStatus()
	v.s.Show()
}

func (v *view) drawRow(y int, r drow) {
	line := v.d.lines[r.line]
	hl := map[int]bool{}
	if v.d.toks[r.line] != nil {
		for _, t := range v.d.toks[r.line] {
			if t.han && t.end > r.start && t.start < r.end &&
				v.rec.words[string(line[t.start:t.end])] {
				for c := t.start; c < t.end; c++ {
					hl[c] = true
				}
			}
		}
	}
	cursor := r.line == v.cl && (v.mode != prompt || v.pchar == ':')
	if cursor && len(line) == 0 {
		v.s.SetContent(0, y, ' ', nil, stCursor)
	}
	_, word := v.lookupTok()
	x := 0
	for c := r.start; c < r.end; c++ {
		st := stDef
		if hl[c] {
			st = stCarded
		}
		if v.selected(r.line, c) {
			st = stSel
		} else if cursor && word.start <= c && c < word.end {
			st = stCurWord
		}
		if cursor && c == v.cc {
			st = stCursor
		}
		v.s.SetContent(x, y, line[c], nil, st)
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
	cx := x
	for i, r := range text {
		rw := zhdict.Width(string(r))
		if cx+rw > v.w && cx > wx {
			cx, y = wx, y+1
		}
		if y >= maxY {
			return y
		}
		if y >= minY {
			v.s.SetContent(cx, y, r, nil, style(i))
		}
		cx += rw
	}
	return y + 1
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
	return styled{x, x, text, func(i int) tcell.Style {
		for k, e := range ends {
			if i < e {
				return sts[k]
			}
		}
		return stDef
	}}
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
	var out []styled
	for i, r := range g.Rows {
		// English is shown on e, or at once when there is no Chinese.
		if i >= g.Zh && !v.showEn && g.Zh > 0 {
			break
		}
		if !v.shows(g, i) {
			continue
		}
		head := []piece{{word, stHead},
			{"  " + v.reading(r[2]), stPinyin}}
		if g.Extra[i] {
			head = append(head, piece{"  " + g.Source(i), stHint})
		}
		if len(out) == 0 && v.rec.words[word] {
			head = append(head, piece{"  (carded)", stHint})
		}
		head = append(head, piece{"  ", stDef})
		col := min(width(head), v.w/2)
		ls := v.senseLines(g.Senses(i), col)
		if len(ls) == 0 {
			ls = []sline{{col, col, nil}}
		}
		for k, l := range ls {
			sl := line(l.x, l.ps...)
			if k == 0 {
				sl = line(0, append(head, l.ps...)...)
			}
			sl.wx = l.wx
			out = append(out, sl)
		}
	}
	return out
}

// shows reports whether row i of g is shown: a fallback dictionary's
// definition of a word the main one defines is shown only on C (MoEDict,
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
	x, wx int
	ps    []piece
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
// indented under their sense.
func (v *view) senseLines(senses []zhdict.Sense, col int) []sline {
	var out []sline
	add := func(x int, pre []piece, ps ...piece) {
		out = append(out, sline{x - width(pre), x, append(pre, ps...)})
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
	}
	return out
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
	{"v V  Enter", "select by character, by line; make a card"},
	{":>cmd", "in v or V: pipe the selection to a shell command"},
	{"m '", "bookmark this line, go to the bookmark"},
	{"e", "show or hide English definitions"},
	{"x", "show or hide quotations (on cards too)"},
	{"C", "show or hide MoEDict beside 兩岸詞典"},
	{"E", "show or hide Wiktionary beside CC-CEDICT"},
	{"t", "show or hide the other reading (Taiwan)"},
	{"J K", "scroll the definitions down, up"},
	{"gh F1  Esc", "show, hide this help"},
	{"q", "quit"},
}

func helpLines() []styled {
	var out []styled
	for _, k := range keyHelp {
		out = append(out, line(0, piece{fmt.Sprintf("%-12s", k[0]),
			stHead}, piece{k[1], stDef}))
	}
	return out
}

// drawPane draws the rule and the pane's lines below it, scrolled by
// pscroll rows, with how many rows are out of view above and below noted
// at the right of the rule.
func (v *view) drawPane(y0 int, lines []styled) {
	total := 0
	for _, l := range lines {
		total += v.height(l.x, l.wx, l.text)
	}
	shown := v.ph - 1
	v.pscroll = max(min(v.pscroll, total-shown), 0)
	for x := 0; x < v.w; x++ {
		v.s.SetContent(x, y0, '─', nil, stRule)
	}
	var more []string
	if v.pscroll > 0 {
		more = append(more, fmt.Sprintf("↑%d", v.pscroll))
	}
	if below := total - shown - v.pscroll; below > 0 {
		more = append(more, fmt.Sprintf("↓%d", below))
	}
	if len(more) > 0 {
		m := " " + strings.Join(more, " ") + " "
		v.put(max(v.w-zhdict.Width(m)-1, 0), y0, y0+1, []rune(m),
			plain(stRule))
	}
	y := y0 + 1 - v.pscroll
	for _, l := range lines {
		y = v.putw(l.x, l.wx, y, y0+1, y0+v.ph, l.text, l.style)
	}
}

func (v *view) drawStatus() {
	y := v.h - 1
	left := v.msg
	switch v.mode {
	case prompt:
		left = string(v.pchar) + string(v.input)
	case visual:
		left = "-- VISUAL --  Enter: make a card"
	case vline:
		left = "-- VISUAL LINE --  Enter: make a card"
	}
	right := fmt.Sprintf(" %d/%d  %s", v.cl+1, len(v.d.lines),
		filepath.Base(v.d.path))
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
	switch v.mode {
	case prompt:
		v.promptKey(ev)
		return
	case card:
		v.cardKey(ev)
		return
	}
	v.msg = ""
	n := max(v.count, 1)
	switch ev.Key() {
	case tcell.KeyEscape:
		v.mode, v.pend, v.count, v.help = normal, 0, 0, false
		return
	case tcell.KeyF1:
		v.toggleHelp()
	case tcell.KeyEnter:
		if v.mode == visual || v.mode == vline {
			v.openCard()
		}
		return
	case tcell.KeyCtrlD:
		v.byRows(n * v.textH() / 2)
	case tcell.KeyCtrlU:
		v.byRows(-n * v.textH() / 2)
	case tcell.KeyCtrlF:
		v.byRows(n * v.textH())
	case tcell.KeyCtrlB:
		v.byRows(-n * v.textH())
	case tcell.KeyDown:
		v.byRows(n)
	case tcell.KeyUp:
		v.byRows(-n)
	case tcell.KeyRune:
		v.runeKey(ev.Rune())
		return
	}
	v.count = 0
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
		v.want = v.cc
	case 'l':
		v.cc += n
		v.clamp()
		v.want = v.cc
	case 'j':
		v.cl, v.cc = v.cl+n, v.want
	case 'k':
		v.cl, v.cc = v.cl-n, v.want
	case 'w':
		for ; n > 0; n-- {
			v.wordFwd()
		}
		v.want = v.cc
	case 'b':
		for ; n > 0; n-- {
			v.wordBack()
		}
		v.want = v.cc
	case '0':
		v.cc, v.want = 0, 0
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
	case 'C':
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
		if v.mode == m {
			v.mode = normal
		} else {
			if v.mode == normal {
				v.al, v.ac = v.cl, v.cc
			}
			v.mode = m
		}
	case 'm':
		if err := v.d.setMark(v.cl); err != nil {
			v.msg = err.Error()
		} else {
			v.msg = fmt.Sprintf("bookmarked line %d", v.cl+1)
		}
	case '\'', '`':
		if l := v.d.getMark(); l >= 0 {
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
		v.want = v.cc
	case '$':
		v.cc = max(row.end-1, row.start)
		v.want = v.cc
	}
}

// byRows moves the cursor n display rows, keeping its screen column.
func (v *view) byRows(n int) {
	ri := v.rowOf(v.cl, v.cc)
	row := v.rows[ri]
	x := zhdict.Width(string(v.d.lines[v.cl][row.start:v.cc]))
	ri = min(max(ri+n, 0), len(v.rows)-1)
	row = v.rows[ri]
	v.cl, v.cc = row.line, row.start
	line := v.d.lines[row.line]
	for cx := 0; v.cc+1 < row.end; v.cc++ {
		cx += zhdict.Width(string(line[v.cc]))
		if cx > x {
			break
		}
	}
}

// wordFwd and wordBack move to the next and previous word, passing over
// punctuation.
func (v *view) wordFwd() {
	l, c := v.cl, v.cc
	for {
		for _, t := range v.d.lineToks(l) {
			if t.start > c && !v.d.punct(l, t) {
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
			if ts[i].start < c && !v.d.punct(l, ts[i]) {
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
			v.cl, v.cc, v.want = l, hit, hit
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

// openCard starts a card for the selection.
func (v *view) openCard() {
	l1, c1, l2, c2 := v.selRange()
	p := v.d.paragraph(l1)
	if l2 >= p.first+len(p.offs) {
		v.msg = "the selection crosses a paragraph break"
		return
	}
	ws, we := p.pos(l1, c1), p.pos(l2, c2)+1
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
	word := string(p.text[ws:we])
	pr := zhdict.NewProbe(zhdict.HanRuns(word))
	pr.Load(v.dict)
	pr.LoadExtra(v.dict) // checked at startup
	pr.LoadPron(v.dict)
	pr.PronPref = v.pron

	// A selection no dictionary has as one word is split into the
	// words that do have definitions, a card each, so selecting a
	// phrase cards its words.
	spans := [][2]int{{ws, we}}
	if len(pr.Rows(word).Rows) == 0 {
		mode := zhdict.SegViterbi
		if pr.LoadFreq(v.dict) != nil {
			mode = zhdict.SegBidi
		}
		seg := func(run []rune) []string {
			return pr.Segment([][]rune{run}, mode)
		}
		spans = nil
		for _, t := range tokenize(p.text[ws:we], seg) {
			w := string(p.text[ws+t.start : ws+t.end])
			if t.han && len(pr.Rows(w).Rows) > 0 {
				spans = append(spans,
					[2]int{ws + t.start, ws + t.end})
			}
		}
		if len(spans) == 0 {
			v.msg = "no definitions for any word in " + word
			return
		}
	}

	// The text is traditional if converting it to simplified changes
	// more of it than converting it to traditional does.
	text := string(p.text)
	trad := changed(text, v.conv.t2s.Convert(text)) >
		changed(text, v.conv.s2t.Convert(text))
	src := fmt.Sprintf("%s:%d", filepath.Base(v.d.path), l1+1)
	v.queue = nil
	for i, sp := range spans {
		ws, we := sp[0], sp[1]
		dr := &draft{p: p, ws: ws, we: we,
			span: newSpan(p.text, ws, we),
			src:  src, trad: trad, n: i + 1, of: len(spans)}
		g := pr.Rows(string(p.text[ws:we]))
		dr.opts, dr.zh = newOpts(g, v.shows)
		dr.notice = g.Notice
		dr.sel = make([]bool, len(dr.opts))
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

// nextCard shows the next card of the selection, or returns to reading
// when there are none left. The duplicate check runs only now, so a word
// the selection repeats is caught against the card just saved for it.
func (v *view) nextCard() {
	if len(v.queue) == 0 {
		v.mode, v.dr = normal, nil
		return
	}
	v.dr, v.queue = v.queue[0], v.queue[1:]
	f := v.dr.forms(v.conv)
	v.dr.dups = v.rec.dups(f.wordS, f.wordT)
	v.mode = card
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

func (v *view) cardKey(ev *tcell.EventKey) {
	dr := v.dr
	r := ev.Rune()
	switch {
	case ev.Key() == tcell.KeyEscape || r == 'q':
		v.mode, v.dr, v.queue = normal, nil, nil
	case r == 'n':
		v.nextCard()
	case ev.Key() == tcell.KeyEnter || r == 'y':
		if len(dr.dups) > 0 && !dr.saving {
			dr.saving = true
			return
		}
		f := dr.forms(v.conv)
		err := v.rec.save(dr.note(f, v.showTW, v.expand), v.deck)
		if err != nil {
			v.msg = err.Error()
		} else {
			v.msg = "saved a card for " + f.wordS
			if f.wordT != f.wordS {
				v.msg += " / " + f.wordT
			}
		}
		v.nextCard()
	case strings.ContainsRune("[]{}", r):
		dr.span.move(r)
		dr.saving = false
	case r == 'j' || ev.Key() == tcell.KeyDown:
		dr.cur = max(min(dr.cur+1, len(dr.opts)-1), 0)
	case r == 'k' || ev.Key() == tcell.KeyUp:
		dr.cur = max(dr.cur-1, 0)
	case r == ' ' && len(dr.sel) > 0:
		dr.sel[dr.cur] = !dr.sel[dr.cur]
	case r == 'x':
		v.expand = !v.expand
	}
}

func (v *view) drawCard() {
	dr := v.dr
	f := dr.forms(v.conv)
	maxY := v.h - 1
	head := " New card: " + f.wordS
	if f.wordT != f.wordS {
		head += " / " + f.wordT
	}
	if dr.of > 1 {
		head += fmt.Sprintf("  (%d of %d)", dr.n, dr.of)
	}
	y := v.put(0, 0, maxY, []rune(head), plain(stHead)) + 1
	ctx := func(label string, rs []rune, ws, we int) {
		lr := []rune(label + "  ")
		style := func(i int) tcell.Style {
			switch i -= len(lr); {
			case i < 0:
				return stHint
			case i >= ws && i < we:
				return stWord
			}
			return stDef
		}
		y = v.put(0, y, maxY, append(lr, rs...), style)
	}
	ctx("简", f.ctxS, f.wsS, f.weS)
	if string(f.ctxT) != string(f.ctxS) {
		ctx("繁", f.ctxT, f.wsT, f.weT)
	}
	y++
	if len(dr.opts) == 0 {
		y = v.put(0, y, maxY, []rune(dr.notice), plain(stWarn))
	}
	// The options scroll to keep the one under the cursor in view,
	// above the cards already made and the help line.
	end := maxY
	if len(dr.dups) > 0 {
		end = max(maxY-len(dr.dups)-2, y+1)
	}
	rows := func(a, b int) int {
		n := 0
		for i := a; i <= b; i++ {
			for _, l := range v.optLines(i) {
				n += v.height(l.x, l.wx, l.text)
			}
		}
		return n
	}
	dr.top = min(dr.top, dr.cur)
	// a row for the note of options scrolled past, once there are any
	for dr.top < dr.cur && y+1+rows(dr.top, dr.cur) > end {
		dr.top++
	}
	if dr.top > 0 {
		y = v.put(0, y, end, []rune(fmt.Sprintf("  ↑ %d more",
			dr.top)), plain(stHint))
	}
	for i := dr.top; i < len(dr.opts) && y < end; i++ {
		for _, l := range v.optLines(i) {
			y = v.putw(l.x, l.wx, y, 0, end, l.text, l.style)
		}
	}
	if len(dr.dups) > 0 {
		y++
		y = v.put(0, y, maxY, []rune(fmt.Sprintf(
			"Already carded (%d):", len(dr.dups))), plain(stWarn))
		for _, k := range dr.dups {
			w := k.simp
			if k.trad != "" && k.trad != k.simp {
				w = strings.TrimPrefix(w+" / "+k.trad, " / ")
			}
			line := w + "  " + k.py + "  "
			if k.ctx != "" {
				line += k.ctx
			} else {
				line += k.def
			}
			line += "  (" + k.src + ")"
			y = v.put(2, y, maxY, clip([]rune(line), v.w-2),
				plain(stDef))
		}
	}
	help := "[ ] left edge  { } right edge  j k space: definitions" +
		"  x: quotations  Enter: save  Esc: cancel"
	if dr.of > 1 {
		help = "[ ] { } context  j k space: definitions" +
			"  x: quotations  Enter: save  n: skip" +
			"  Esc: cancel the rest"
	}
	st := stHint
	if dr.saving {
		help, st = "Already carded. Enter: save anyway  Esc: cancel",
			stWarn
	}
	v.put(0, v.h-1, v.h, clip([]rune(help), v.w), plain(st))
}

// optLines are the lines option i takes in the card preview: a blank one
// before the first English option, the option itself, and the quotations
// that go on the card with it after x.
func (v *view) optLines(i int) []styled {
	dr, o := v.dr, v.dr.opts[i]
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
