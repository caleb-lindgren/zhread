package main

import (
	"fmt"
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
	rows     []drow
	firstRow []int // index into rows of each line's first row
	top      int   // first row on screen

	cl, cc int // cursor line and column
	want   int // column j and k aim for
	al, ac int // visual anchor
	mode   int
	pend   rune // first key of a two-key command (g)
	count  int
	showEn bool
	msg    string

	pat    *regexp.Regexp
	back   bool // last search went backward
	input  []rune
	inBack bool
	dr     *draft
	quit   bool
}

var (
	stDef    = tcell.StyleDefault
	stCarded = stDef.Foreground(tcell.ColorOlive)
	stSel    = stDef.Reverse(true)
	stHead   = stDef.Bold(true)
	stDim    = stDef.Foreground(tcell.ColorGray)
	stWarn   = stDef.Foreground(tcell.ColorMaroon).Bold(true)
	stWord   = stDef.Bold(true).Underline(true)
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
			v.d.apply(ev.Data().(segRes))
		case *tcell.EventKey:
			v.key(ev)
		}
	}
	return nil
}

// layout: the text above, the definitions pane below it, a status line.
func (v *view) textH() int { return v.h - 1 - v.paneH() }
func (v *view) paneH() int {
	if v.h < 10 {
		return 0
	}
	return min(max(v.h/3, 4), 14)
}

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

// scroll keeps the cursor's row on screen with two rows of margin.
func (v *view) scroll() {
	th := v.textH()
	r := v.rowOf(v.cl, v.cc)
	off := min(2, (th-1)/2)
	if r-off < v.top {
		v.top = max(r-off, 0)
	}
	if r+off >= v.top+th {
		v.top = r + off - th + 1
	}
	v.top = min(v.top, max(len(v.rows)-th, 0))
}

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
	v.scroll()
	th := v.textH()
	for y := 0; y < th && v.top+y < len(v.rows); y++ {
		v.drawRow(y, v.rows[v.top+y])
	}
	v.drawPane(th)
	v.drawStatus()
	r := v.rowOf(v.cl, v.cc)
	row := v.rows[r]
	x := zhdict.Width(string(v.d.lines[v.cl][row.start:v.cc]))
	v.s.ShowCursor(x, r-v.top)
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
	x := 0
	for c := r.start; c < r.end; c++ {
		st := stDef
		if hl[c] {
			st = stCarded
		}
		if v.selected(r.line, c) {
			st = stSel
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
	cx := x
	for i, r := range text {
		rw := zhdict.Width(string(r))
		if cx+rw > v.w && cx > x {
			cx, y = x, y+1
		}
		if y >= maxY {
			return y
		}
		v.s.SetContent(cx, y, r, nil, style(i))
		cx += rw
	}
	return y + 1
}

func plain(st tcell.Style) func(int) tcell.Style {
	return func(int) tcell.Style { return st }
}

// lookup is the word the definitions pane shows: the selection, or the
// word under the cursor, with the probe that can look it up.
func (v *view) lookup() (string, *zhdict.Probe) {
	if v.mode == visual || v.mode == vline {
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

func (v *view) drawPane(y0 int) {
	ph := v.paneH()
	if ph == 0 {
		return
	}
	maxY := y0 + ph
	for x := 0; x < v.w; x++ {
		v.s.SetContent(x, y0, '─', nil, stDim)
	}
	word, p := v.lookup()
	if word == "" {
		return
	}
	head := " " + word + " "
	if v.rec.words[word] {
		head += "(carded) "
	}
	v.put(1, y0, y0+1, []rune(head), plain(stHead))
	y := y0 + 1
	if p == nil {
		v.put(0, y, maxY, []rune("…"), plain(stDim))
		return
	}
	g := p.Rows(word)
	if g.Notice != "" {
		v.put(0, y, maxY, []rune(g.Notice), plain(stDim))
		return
	}
	for i, r := range g.Rows {
		if i >= g.Zh && !v.showEn {
			break
		}
		py := []rune(r[2] + "  ")
		y = v.put(0, y, maxY, append(py, []rune(r[3])...),
			func(i int) tcell.Style {
				if i < len(py) {
					return stHead
				}
				return stDef
			})
	}
	if !v.showEn && len(g.Rows) > g.Zh {
		v.put(0, y, maxY, []rune(fmt.Sprintf("(e: %d English)",
			len(g.Rows)-g.Zh)), plain(stDim))
	}
}

func (v *view) drawStatus() {
	y := v.h - 1
	left := v.msg
	switch v.mode {
	case prompt:
		c := "/"
		if v.inBack {
			c = "?"
		}
		left = c + string(v.input)
	case visual:
		left = "-- VISUAL --  Enter: make a card"
	case vline:
		left = "-- VISUAL LINE --  Enter: make a card"
	}
	right := fmt.Sprintf(" %d/%d  %s", v.cl+1, len(v.d.lines),
		filepath.Base(v.d.path))
	v.put(0, y, y+1, []rune(left), plain(stDef))
	rx := v.w - zhdict.Width(right)
	v.put(max(rx, 0), y, y+1, []rune(right), plain(stDim))
	if v.mode == prompt {
		v.s.ShowCursor(zhdict.Width(left), y)
	}
}

// selRange is the selection, start before end, end column inclusive.
func (v *view) selRange() (l1, c1, l2, c2 int) {
	l1, c1, l2, c2 = v.al, v.ac, v.cl, v.cc
	if l2 < l1 || l2 == l1 && c2 < c1 {
		l1, c1, l2, c2 = l2, c2, l1, c1
	}
	if v.mode == vline {
		c1, c2 = 0, max(v.lineLen(l2)-1, 0)
	}
	c2 = min(c2, max(v.lineLen(l2)-1, 0))
	return
}

func (v *view) selected(l, c int) bool {
	if v.mode != visual && v.mode != vline {
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
		v.mode, v.pend, v.count = normal, 0, 0
		return
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
		v.mode, v.input, v.inBack = prompt, nil, r == '?'
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
	case 'q':
		if v.mode == normal {
			v.quit = true
		}
	}
}

func (v *view) gKey(r rune) {
	n, counted := max(v.count, 1), v.count > 0
	v.count = 0
	row := v.rows[v.rowOf(v.cl, v.cc)]
	switch r {
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

// byRows moves the cursor n display rows, keeping its screen column, and
// scrolls the screen with it.
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
	if n > 1 || n < -1 {
		v.top = min(max(v.top+n, 0), max(len(v.rows)-v.textH(), 0))
	}
}

func (v *view) wordFwd() {
	l, c := v.cl, v.cc
	for {
		for _, t := range v.d.lineToks(l) {
			if t.start > c {
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
			if ts[i].start < c {
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

func (v *view) promptKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEscape:
		v.mode = normal
	case tcell.KeyEnter:
		v.mode = normal
		if len(v.input) == 0 && v.pat != nil {
			v.back = v.inBack
			v.search(v.back)
			return
		}
		re, err := regexp.Compile(string(v.input))
		if err != nil {
			v.msg = err.Error()
			return
		}
		v.pat, v.back = re, v.inBack
		v.search(v.back)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(v.input) > 0 {
			v.input = v.input[:len(v.input)-1]
		} else {
			v.mode = normal
		}
	case tcell.KeyRune:
		v.input = append(v.input, ev.Rune())
	}
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
	pr.LoadPron(v.dict)
	pr.PronPref = v.pron
	dr := &draft{p: p, ws: ws, we: we, span: newSpan(p.text, ws, we),
		defs: pr.Rows(word),
		src:  fmt.Sprintf("%s:%d", filepath.Base(v.d.path), l1+1)}
	dr.sel = make([]bool, len(dr.defs.Rows))
	if dr.defs.Zh > 0 {
		dr.sel[0] = true
	}
	if len(dr.defs.Rows) > dr.defs.Zh {
		dr.sel[dr.defs.Zh] = true
	}
	// The text is traditional if converting it to simplified changes
	// more of it than converting it to traditional does.
	text := string(p.text)
	dr.trad = changed(text, v.conv.t2s.Convert(text)) >
		changed(text, v.conv.s2t.Convert(text))
	f := dr.forms(v.conv)
	dr.dups = v.rec.dups(f.wordS, f.wordT)
	v.dr, v.mode = dr, card
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
		v.mode, v.dr = normal, nil
	case ev.Key() == tcell.KeyEnter || r == 'y':
		if len(dr.dups) > 0 && !dr.saving {
			dr.saving = true
			return
		}
		f := dr.forms(v.conv)
		if err := v.rec.save(dr.note(f), v.deck); err != nil {
			v.msg = err.Error()
		} else {
			v.msg = "saved a card for " + f.wordS
			if f.wordT != f.wordS {
				v.msg += " / " + f.wordT
			}
		}
		v.mode, v.dr = normal, nil
	case strings.ContainsRune("[]{}", r):
		dr.span.move(r)
		dr.saving = false
	case r == 'j' || ev.Key() == tcell.KeyDown:
		dr.cur = min(dr.cur+1, len(dr.defs.Rows)-1)
	case r == 'k' || ev.Key() == tcell.KeyUp:
		dr.cur = max(dr.cur-1, 0)
	case r == ' ' && len(dr.sel) > 0:
		dr.sel[dr.cur] = !dr.sel[dr.cur]
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
	y := v.put(0, 0, maxY, []rune(head), plain(stHead)) + 1
	ctx := func(label string, rs []rune, ws, we int) {
		lr := []rune(label + "  ")
		style := func(i int) tcell.Style {
			switch i -= len(lr); {
			case i < 0:
				return stDim
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
	if len(dr.defs.Rows) == 0 {
		y = v.put(0, y, maxY, []rune(dr.defs.Notice), plain(stWarn))
	}
	for i, r := range dr.defs.Rows {
		box := "[ ] "
		if dr.sel[i] {
			box = "[x] "
		}
		text := []rune(box + r[2] + "  " + r[3])
		st := stDef
		if i == dr.cur {
			st = stSel
		} else if !dr.sel[i] {
			st = stDim
			// one row each for the rest, so the list stays short
			text = clip(text, v.w)
		}
		if i == dr.Zh() && i > 0 {
			y++
		}
		y = v.put(0, y, maxY, text, plain(st))
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
		"  Enter: save  Esc: cancel"
	st := stDim
	if dr.saving {
		help, st = "Already carded. Enter: save anyway  Esc: cancel",
			stWarn
	}
	v.put(0, v.h-1, v.h, clip([]rune(help), v.w), plain(st))
}

func (dr *draft) Zh() int { return dr.defs.Zh }

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
