package main

import (
	"cmp"
	"crypto/sha1"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"html"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/caleb-lindgren/ankitools/internal/zhdict"
)

// Context length bounds, in characters. A sentence shorter than minCtx
// (「好。」) is too little to recall a word from, so the neighbouring
// sentence is added; one longer than maxCtx is trimmed to the clauses
// around the word. Both are guesses to adjust after some use.
const (
	minCtx = 12
	maxCtx = 60
)

const (
	enders  = "。！？!?…"
	seps    = "，；：、,;:"
	closers = "”’」』）》】〕\"')"
)

// clauses splits t after every clause or sentence punctuation mark, along
// with any closing quotes or brackets that follow it. bounds[0] is 0 and
// the last is len(t); sentEnd[k] says whether bounds[k] ends a sentence.
func clauses(t []rune) (bounds []int, sentEnd []bool) {
	bounds, sentEnd = []int{0}, []bool{true}
	for i := 0; i < len(t); i++ {
		r := t[i]
		end := strings.ContainsRune(enders, r)
		if !end && !strings.ContainsRune(seps, r) {
			continue
		}
		j := i + 1
		for j < len(t) && (strings.ContainsRune(closers, t[j]) ||
			strings.ContainsRune(enders, t[j])) {
			end = end || strings.ContainsRune(enders, t[j])
			j++
		}
		bounds = append(bounds, j)
		sentEnd = append(sentEnd, end)
		i = j - 1
	}
	if bounds[len(bounds)-1] != len(t) {
		bounds = append(bounds, len(t))
		sentEnd = append(sentEnd, true)
	}
	return bounds, sentEnd
}

// span is the context: clauses [a, b) of a paragraph, which must include
// clauses [ca, cb) holding the word.
type span struct {
	bounds  []int
	sentEnd []bool
	a, b    int
	ca, cb  int
}

func newSpan(t []rune, ws, we int) span {
	bounds, sentEnd := clauses(t)
	return spanIn(bounds, sentEnd, ws, we)
}

// spanIn is newSpan with the text's clauses, as clauses splits it, worked
// out already: a paragraph with the word many times is split once. The
// spans share bounds and sentEnd, which they only read.
func spanIn(bounds []int, sentEnd []bool, ws, we int) span {
	s := span{bounds: bounds, sentEnd: sentEnd}
	// the last clause starting at or before ws, and the first after it
	// ending at or after we
	s.ca = min(sort.SearchInts(bounds, ws+1)-1, len(bounds)-1)
	s.cb = max(s.ca+1, min(sort.SearchInts(bounds, we), len(bounds)-1))
	s.a, s.b = s.ca, s.cb
	s.sentence()
	for s.len() < minCtx && (s.a > 0 || s.b < s.n()) {
		if s.a > 0 {
			s.a--
		} else {
			s.b++
		}
		s.sentence()
	}
	for s.len() > maxCtx {
		left := bounds[s.ca] - bounds[s.a]
		right := bounds[s.b] - bounds[s.cb]
		switch {
		case s.a < s.ca && left >= right:
			s.a++
		case s.b > s.cb:
			s.b--
		case s.a < s.ca:
			s.a++
		default:
			return s
		}
	}
	return s
}

func (s *span) n() int { return len(s.bounds) - 1 }

// ends are the offsets in the paragraph of the span's start and end.
func (s *span) ends() (int, int) { return s.bounds[s.a], s.bounds[s.b] }
func (s *span) len() int         { return s.bounds[s.b] - s.bounds[s.a] }

// sentence widens the span to whole sentences.
func (s *span) sentence() {
	for s.a > 0 && !s.sentEnd[s.a] {
		s.a--
	}
	for s.b < s.n() && !s.sentEnd[s.b] {
		s.b++
	}
}

// move shifts one edge by a clause: [ and ] the left edge, { and } the
// right, each left and right respectively.
func (s *span) move(key rune) {
	switch {
	case key == '[' && s.a > 0:
		s.a--
	case key == ']' && s.a < s.ca:
		s.a++
	case key == '{' && s.b > s.cb:
		s.b--
	case key == '}' && s.b < s.n():
		s.b++
	}
}

// record is everything already carded: the cards in cards.tsv.
type record struct {
	path  string // cards.tsv
	cards []carded
	words map[string]bool // both forms of every word, for highlighting
	// senses holds each carded sense as word + "\x1f" + its html, under
	// both forms of the word, so the pane can mark which were carded.
	senses map[string]bool
}

type carded struct {
	simp, trad, py, def, ctx, src string
	en                            string // the English definitions
	guid                          string // its row's GUID in cards.tsv
}

// Columns of cards.tsv, after the GUID: the note type's fields in order.
// Contexts and ContextsTrad, the word's other contexts in the book, come
// last, so the columns of cards made before them keep their places.
var fields = []string{"Simplified", "Traditional", "Pinyin", "DefZh",
	"DefEn", "Context", "Cloze", "ContextTrad", "ClozeTrad", "Source",
	"Added", "Contexts", "ContextsTrad"}

func loadRecord(cardsPath string) (*record, error) {
	rec := &record{path: cardsPath, words: map[string]bool{},
		senses: map[string]bool{}}
	err := readTSV(cardsPath, func(f []string) {
		if len(f) >= 12 {
			rec.add(cardOf(f))
		}
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (rec *record) add(k carded) {
	rec.cards = append(rec.cards, k)
	for _, w := range []string{k.simp, k.trad} {
		if w == "" {
			continue
		}
		rec.words[w] = true
		// A definition field is its senses' html joined by <br>, each
		// followed by its quotations, if any, also joined by <br>.
		for _, d := range []string{k.def, k.en} {
			for _, h := range strings.Split(d, "<br>") {
				if h != "" {
					rec.senses[w+"\x1f"+h] = true
				}
			}
		}
	}
}

// cardedSense reports whether sense s of word is on a card.
func (rec *record) cardedSense(word string, s zhdict.Sense) bool {
	return rec.senses[word+"\x1f"+opt{s: s}.html(false)]
}

// readTSV reads a file of tab-separated records, skipping Anki's header
// lines; a missing file has no records.
func readTSV(path string, f func([]string)) error {
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.Comma, r.Comment = '\t', '#'
	r.FieldsPerRecord, r.LazyQuotes = -1, true
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		f(rec)
	}
}

// dups are the cards whose word equals, contains, or is contained in
// either form of the new one.
func (rec *record) dups(forms ...string) []carded {
	var out []carded
	for _, k := range rec.cards {
	match:
		for _, f := range forms {
			for _, g := range []string{k.simp, k.trad} {
				if f == "" || g == "" {
					continue
				}
				if strings.Contains(f, g) ||
					strings.Contains(g, f) {
					out = append(out, k)
					break match
				}
			}
		}
	}
	return out
}

// draft is a card being made.
type draft struct {
	p      para
	ws, we int // the word, as offsets into p.text
	span   span
	g      zhdict.Group // the word's definitions, which opts are from
	opts   []opt
	zh     int    // how many of opts, from the start, are Chinese
	notice string // stands in for opts when there are none
	sel    []bool // which of opts go on the card
	// cur is the option under the cursor, or with the cursor on a
	// context, its line counted back from the options: -1 the line just
	// above them, -2 the one above that.
	cur  int
	top  int // the first option in view
	dups []carded
	// exact are the dups for the word itself, which u adds the card to
	// instead of saving it as a new one; which is set while u asks
	// which of several.
	exact  []carded
	which  bool
	src    string
	trad   bool // the text is traditional, so simplified is converted
	saving bool // the duplicate warning has been shown once
	n, of  int  // this is card n of the selection's of
	// ed holds a context as edited, simplified then traditional, in
	// place of the one the span and the conversion give.
	ed   [2]*edited
	vi   vi     // the editor on the context under the cursor
	warn string // why Enter did not save
	// others are the other contexts of a card being edited, drafts of
	// their own, which the card shows in turn with its own, and osel
	// which of them stay on it; a new card has none. The cursor goes
	// onto them after the options: others[k] is at len(opts)+k. i edits
	// the one under it, which editing says.
	others  []*draft
	osel    []bool
	editing bool
	esc     bool  // the last key was Esc
	gPend   bool  // g was pressed, for gh
	discard bool  // asking whether to throw the changes away
	changed bool  // an edge has moved or a tick changed
	ctxf    forms // of another context: its forms, kept by refresh
	// from, for another context of a card being edited, is the label of
	// where it came from that the card has after it, kept as it is.
	from string
	// guid, for a card being edited, is its row of cards.tsv, which saving
	// overwrites, keeping added, the date it was first made; raws are its
	// definitions no dictionary has, as opts.
	guid  string
	added string
	raws  []opt
}

// refresh works out again the forms of another context, after it changes.
func (dr *draft) refresh(conv convs) { dr.ctxf = dr.forms(conv) }

// own is the index into ed of the text's script.
func (dr *draft) own() int {
	if dr.trad {
		return 1
	}
	return 0
}

// edited reports whether the card has been changed by hand: a context,
// the card's own or another, edited or its edges moved, or a definition
// or context ticked or unticked.
func (dr *draft) edited() bool {
	return dr.changed || dr.ed != [2]*edited{} ||
		slices.ContainsFunc(dr.others,
			func(o *draft) bool { return o.ed != [2]*edited{} })
}

// curVi is the editor's state on the context under the cursor.
func (dr *draft) curVi() *vi {
	if o := dr.other(); o != nil {
		return &o.vi
	}
	return &dr.vi
}

// other is the other context under the cursor, if it is on one.
func (dr *draft) other() *draft {
	if k := dr.cur - len(dr.opts); dr.cur >= 0 && k >= 0 &&
		k < len(dr.others) {
		return dr.others[k]
	}
	return nil
}

// rows is how many lines of the preview the cursor goes over below the
// context: the options, then the other contexts.
func (dr *draft) rows() int { return len(dr.opts) + len(dr.others) }

// tick ticks or unticks the option or other context at i.
func (dr *draft) tick(i int) {
	if i < len(dr.opts) {
		dr.sel[i] = !dr.sel[i]
	} else {
		dr.osel[i-len(dr.opts)] = !dr.osel[i-len(dr.opts)]
	}
}

// edited is a context changed by hand, with where the word is in it.
type edited struct {
	text   []rune
	ws, we int
}

// opt is one definition to choose for the card: one sense of it, so a
// card can carry the sense the context uses rather than all twenty of
// 打's.
type opt struct {
	py     string // the reading, with the other side's noted
	s      zhdict.Sense
	src    string // the dictionary, named for a fallback's definitions
	row, k int    // its row of the group and its sense in the row
	// raw, for a definition of a card being edited that no dictionary
	// shown has, is its HTML as the card has it, kept as it is. Its row is
	// -1 for a Chinese one, -2 for an English one.
	raw string
}

// newOpts splits a word's definitions, those shows lets through, into
// options, Chinese first.
func newOpts(g zhdict.Group,
	shows func(zhdict.Group, int) bool) (opts []opt, zh int) {
	for i, r := range g.Rows {
		if i == g.Zh {
			zh = len(opts)
		}
		if !shows(g, i) {
			continue
		}
		src := ""
		if g.Extra[i] {
			src = g.Source(i)
		}
		for k, s := range g.Senses(i) {
			opts = append(opts, opt{py: r[2], s: s, src: src,
				row: i, k: k})
		}
	}
	if g.Zh == len(g.Rows) {
		zh = len(opts)
	}
	return opts, zh
}

// setOpts makes the options from the group again, after c or E has changed
// which dictionaries are shown, keeping the ones ticked and the one under
// the cursor.
func (dr *draft) setOpts(shows func(zhdict.Group, int) bool) {
	type key struct{ row, k int }
	// a cursor on the other contexts stays on the same one
	other := -1
	if dr.cur >= len(dr.opts) && len(dr.others) > 0 {
		other = dr.cur - len(dr.opts)
	}
	ticked := map[key]bool{}
	var at *key
	for i, o := range dr.opts {
		if dr.sel[i] {
			ticked[key{o.row, o.k}] = true
		}
		if i == dr.cur {
			at = &key{o.row, o.k}
		}
	}
	for i, o := range dr.opts {
		if o.raw != "" {
			dr.raws[o.k] = o
			ticked[key{o.row, o.k}] = dr.sel[i]
		}
	}
	opts, zh := newOpts(dr.g, shows)
	// a card's own definitions that no dictionary has, after the rest of
	// their language
	var zhRaw, enRaw []opt
	for _, o := range dr.raws {
		if o.row == -1 {
			zhRaw = append(zhRaw, o)
		} else {
			enRaw = append(enRaw, o)
		}
	}
	dr.opts = slices.Concat(opts[:zh], zhRaw, opts[zh:], enRaw)
	dr.zh = zh + len(zhRaw)
	dr.sel = make([]bool, len(dr.opts))
	cur := min(dr.cur, len(dr.opts)-1)
	for i, o := range dr.opts {
		dr.sel[i] = ticked[key{o.row, o.k}]
		if at != nil && *at == (key{o.row, o.k}) {
			cur = i
		}
	}
	if dr.cur >= 0 {
		dr.cur = max(cur, 0)
	}
	if other >= 0 {
		dr.cur = len(dr.opts) + other
	}
	dr.top = min(dr.top, max(dr.cur, 0))
}

// html is the option as it goes on the card: its part of speech, heading,
// text, examples and synonym lists on one line, and its quotations on lines
// after it when cites is set.
func (o opt) html(cites bool) string {
	if o.raw != "" {
		return o.raw
	}
	var ps []string
	parts := append(append([]string{o.s.Pos}, o.s.Heads...), o.s.Text)
	for _, t := range append(parts,
		append(o.s.Ex, o.s.Rels...)...) {
		if t != "" {
			ps = append(ps, html.EscapeString(t))
		}
	}
	out := strings.Join(ps, " ")
	if cites {
		for _, c := range o.s.Cites {
			out += "<br>" + html.EscapeString(c)
		}
	}
	return out
}

// context returns the context's text and the word's offsets in it, with
// whitespace at either end trimmed. Where the context stops short of the end
// of a sentence, the comma or other clause mark it ends at is dropped:
// 「…四奔逃散」 rather than 「…四奔逃散，」.
func (dr *draft) context() ([]rune, int, int) {
	s := dr.span
	a, b := s.bounds[s.a], s.bounds[s.b]
	trim := func() {
		for b > dr.we && unicode.IsSpace(dr.p.text[b-1]) {
			b--
		}
	}
	for a < dr.ws && unicode.IsSpace(dr.p.text[a]) {
		a++
	}
	trim()
	if !s.sentEnd[s.b] {
		for b > dr.we && strings.ContainsRune(seps, dr.p.text[b-1]) {
			b--
		}
		trim()
	}
	return dr.p.text[a:b], dr.ws - a, dr.we - a
}

// forms gives the context and word in both scripts. OpenCC converts the
// whole context, since a character's conversion depends on its phrase (头发
// is 頭髮, 发现 is 發現), and the word is cut from the result at the same
// offsets. Conversion keeps the length in all but rare cases; when it does
// not, the word is converted on its own.
type forms struct {
	ctxS, ctxT   []rune
	wsS, weS     int
	wsT, weT     int
	wordS, wordT string
}

func (dr *draft) forms(conv convs) forms {
	// indices into ed of the text's script and of the other
	own, other := 0, 1
	if dr.trad {
		own, other = 1, 0
	}
	ctx, ws, we := dr.context()
	if e := dr.ed[own]; e != nil {
		ctx, ws, we = e.text, e.ws, e.we
	}
	toOther := conv.s2t.Convert
	if dr.trad {
		toOther = conv.t2s.Convert
	}
	oc := []rune(toOther(string(ctx)))
	ows, owe := ws, we
	if len(oc) != len(ctx) {
		pre := []rune(toOther(string(ctx[:ws])))
		w := []rune(toOther(string(ctx[ws:we])))
		post := []rune(toOther(string(ctx[we:])))
		oc = append(append(append([]rune{}, pre...), w...), post...)
		ows, owe = len(pre), len(pre)+len(w)
	}
	if e := dr.ed[other]; e != nil {
		oc, ows, owe = e.text, e.ws, e.we
	}
	f := forms{ctxS: ctx, wsS: ws, weS: we, ctxT: oc, wsT: ows, weT: owe}
	if dr.trad {
		f.ctxS, f.ctxT = f.ctxT, f.ctxS
		f.wsS, f.wsT, f.weS, f.weT = f.wsT, f.wsS, f.weT, f.weS
	}
	f.wordS = string(f.ctxS[f.wsS:f.weS])
	f.wordT = string(f.ctxT[f.wsT:f.weT])
	return f
}

// note builds the card's fields. The traditional ones are left empty when
// the word is the same in both scripts, which is what keeps Anki from
// generating the traditional cards; when the text is traditional, it is
// the simplified contexts instead that are left empty, so the one set of
// cards made is in the text's script. The reading carries the other side's
// only when both is set, as the pane shows it only after t, and the
// quotations go on only when cites is, as the pane shows them after x. The
// reading is followed by its zhuyin. When cards are made in both scripts, a
// cloze's blank says which script it asks for, [简体] or [繁體]. The ticked
// other contexts go in the last two fields, each in a div with the word in
// bold, left empty as the contexts are; the card makes their clozes itself.
func (dr *draft) note(f forms, both, cites bool) []string {
	var py string
	var zh, en []string
	for i, o := range dr.opts {
		if !dr.sel[i] {
			continue
		}
		if py == "" {
			py = o.py
			if !both {
				py = zhdict.Preferred(py)
			}
			py = withZhuyin(py)
		}
		if i < dr.zh {
			zh = append(zh, o.html(cites))
		} else {
			en = append(en, o.html(cites))
		}
	}
	ctxS := mark(f.ctxS, f.wsS, f.weS, "")
	clozeS := mark(f.ctxS, f.wsS, f.weS, gap)
	var more, moreT string
	for i, d := range dr.others {
		o := d.ctxf
		// an edit can make the context the same as one of them
		if !dr.osel[i] || slices.Equal(o.ctxS, f.ctxS) &&
			slices.Equal(o.ctxT, f.ctxT) {
			continue
		}
		more += "<div>" + mark(o.ctxS, o.wsS, o.weS, "") +
			srcLabel(d.from) + "</div>"
		moreT += "<div>" + mark(o.ctxT, o.wsT, o.weT, "") +
			srcLabel(d.from) + "</div>"
	}
	var trad, ctxT, clozeT string
	if f.wordT == f.wordS && !dr.trad {
		moreT = ""
	}
	if f.wordT != f.wordS || dr.trad {
		trad = f.wordT
		ctxT = mark(f.ctxT, f.wsT, f.weT, "")
		clozeT = mark(f.ctxT, f.wsT, f.weT, gap)
		if f.wordT == f.wordS {
			ctxS, clozeS, more = "", "", ""
		} else {
			clozeS = mark(f.ctxS, f.wsS, f.weS, gapS)
			clozeT = mark(f.ctxT, f.wsT, f.weT, gapT)
		}
	}
	sum := sha1.Sum([]byte(f.wordS + "\x1f" + string(f.ctxS)))
	return []string{
		base64.RawURLEncoding.EncodeToString(sum[:])[:12],
		f.wordS, trad, py,
		strings.Join(zh, "<br>"), strings.Join(en, "<br>"),
		ctxS, clozeS, ctxT, clozeT,
		html.EscapeString(dr.src), time.Now().Format("2006-01-02"),
		more, moreT,
	}
}

// readingNote matches the other side's reading noted after a reading, as
// zhdict's readings writes it: "qǐ yè (TW qì yè)".
var readingNote = regexp.MustCompile(`^(.*) \((TW|CN) ([^()]*)\)$`)

// withZhuyin is a reading with its zhuyin after it, the other side's
// reading too: "qǐ yè　ㄑㄧˇ ㄧㄝˋ (TW qì yè　ㄑㄧˋ ㄧㄝˋ)". A reading that
// will not convert is left as it is.
func withZhuyin(py string) string {
	one := func(p string) string {
		if z := zhdict.Zhuyin(p); z != "" {
			return p + "　" + z
		}
		return p
	}
	if m := readingNote.FindStringSubmatch(py); m != nil {
		return one(m[1]) + " (" + m[2] + " " + one(m[3]) + ")"
	}
	return one(py)
}

// The blanks of a cloze card: the word, or when the note has cards in both
// scripts, the script asked for.
const (
	gap  = "[…]"
	gapS = "[简体]"
	gapT = "[繁體]"
)

// mark renders a context as HTML with the word in bold, or for the cloze
// card, with cloze set, blanked as it says. Any other occurrence of the
// word is bolded or blanked the same way: the cloze would otherwise give
// the word away, and the answer should show every place the cloze
// blanked. A doubled form of the word is marked whole, 松松垮垮 for 松垮.
func mark(ctx []rune, ws, we int, cloze string) string {
	word := html.EscapeString(string(ctx[ws:we]))
	ws, we = doubled(ctx, ws, we)
	blank := `<span class=cloze>` + cloze + `</span>`
	if cloze == "" {
		blank = `<b>` + word + `</b>`
	}
	esc := func(rs []rune) string {
		s := html.EscapeString(string(rs))
		if word == "" {
			return s
		}
		return strings.ReplaceAll(s, word, blank)
	}
	mid := `<b>` + html.EscapeString(string(ctx[ws:we])) + `</b>`
	if cloze != "" {
		mid = blank
	}
	return esc(ctx[:ws]) + mid + esc(ctx[we:])
}

// exact are the cards of dups for the word itself, in either script,
// rather than for a word containing it or contained in it: the cards Enter
// can add to instead of making a new one.
func exact(dups []carded, f forms) []carded {
	var out []carded
	for _, k := range dups {
		if k.simp == f.wordS || k.trad != "" && k.trad == f.wordT {
			out = append(out, k)
		}
	}
	return out
}

// divs matches each context of the Contexts fields, without the label
// of where it came from.
var divs = regexp.MustCompile(`<div>(.*?)(?: <span class=src>.*?</span>)?` +
	`</div>`)

// srcLabel is the label a context found outside the book carries on the
// card, after it, saying where it came from.
func srcLabel(from string) string {
	if from == "" {
		return ""
	}
	return ` <span class=src>` + html.EscapeString(from) + `</span>`
}

// moveOther moves the other context under the cursor a place down, or up,
// in the order the card shows them, the cursor with it.
func (dr *draft) moveOther(down bool) {
	k := dr.cur - len(dr.opts)
	j := k - 1
	if down {
		j = k + 1
	}
	if k < 0 || j < 0 || j >= len(dr.others) {
		return
	}
	dr.others[k], dr.others[j] = dr.others[j], dr.others[k]
	dr.osel[k], dr.osel[j] = dr.osel[j], dr.osel[k]
	dr.cur += j - k
	dr.changed = true
}

// update rewrites the row of cards.tsv with the GUID guid as change
// leaves it, padded first to every column, and reads the record again.
func (rec *record) update(guid string, change func([]string)) error {
	b, err := os.ReadFile(rec.path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, "#") || !strings.HasPrefix(l,
			guid+"\t") {
			continue
		}
		r := csv.NewReader(strings.NewReader(l))
		r.Comma, r.FieldsPerRecord, r.LazyQuotes = '\t', -1, true
		row, err := r.Read()
		if err != nil || row[0] != guid {
			continue
		}
		for len(row) < 1+len(fields) {
			row = append(row, "")
		}
		change(row)
		var sb strings.Builder
		if err := writeRow(&sb, row); err != nil {
			return err
		}
		lines[i] = sb.String()
		found = true
		break
	}
	if !found {
		return errors.New("no card " + guid + " in " + rec.path)
	}
	tmp := rec.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "")),
		0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, rec.path); err != nil {
		return err
	}
	fresh, err := loadRecord(rec.path)
	if err != nil {
		return err
	}
	*rec = *fresh
	return nil
}

// writeRow writes a row of cards.tsv, with tabs and line breaks in its
// fields made spaces.
func writeRow(w io.Writer, row []string) error {
	cw := csv.NewWriter(w)
	cw.Comma = '\t'
	for i := range row {
		row[i] = strings.Map(func(r rune) rune {
			if r == '\t' || r == '\n' || r == '\r' {
				return ' '
			}
			return r
		}, row[i])
	}
	if err := cw.Write(row); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// save appends the note to cards.tsv, writing Anki's import headers first
// when the file is new. The GUID column is what lets the whole file be
// imported again: Anki updates the notes it already has instead of adding
// them twice.
func (rec *record) save(note []string, deck string) error {
	_, err := os.Stat(rec.path)
	fresh := errors.Is(err, fs.ErrNotExist)
	fh, err := os.OpenFile(rec.path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	if fresh {
		hdr := "#separator:tab\n#html:true\n#notetype:zhread\n" +
			"#deck:" + deck + "\n#guid column:1\n#columns:GUID\t" +
			strings.Join(fields, "\t") + "\n"
		if _, err := fh.WriteString(hdr); err != nil {
			return err
		}
	}
	if err := writeRow(fh, note); err != nil {
		return err
	}
	rec.add(cardOf(note))
	return nil
}

// cardOf is the record of a row of cards.tsv, GUID first.
func cardOf(f []string) carded {
	return carded{simp: f[1], trad: f[2], py: f[3], def: f[4],
		ctx: strip(cmp.Or(f[6], f[8])), src: f[10], en: f[5],
		guid: f[0]}
}

// strip removes the HTML tags of a context field, for display.
func strip(s string) string {
	var sb strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			sb.WriteRune(r)
		}
	}
	return html.UnescapeString(sb.String())
}
