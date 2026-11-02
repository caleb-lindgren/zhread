package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"html"
	"io"
	"io/fs"
	"os"
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
	s := span{bounds: bounds, sentEnd: sentEnd}
	for s.ca+1 < len(bounds) && bounds[s.ca+1] <= ws {
		s.ca++
	}
	s.cb = s.ca + 1
	for s.cb < len(bounds)-1 && bounds[s.cb] < we {
		s.cb++
	}
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

func (s *span) n() int   { return len(s.bounds) - 1 }
func (s *span) len() int { return s.bounds[s.b] - s.bounds[s.a] }

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
}

// Columns of cards.tsv, after the GUID: the note type's fields in order.
var fields = []string{"Simplified", "Traditional", "Pinyin", "DefZh",
	"DefEn", "Context", "Cloze", "ContextTrad", "ClozeTrad", "Source",
	"Added"}

func loadRecord(cardsPath string) (*record, error) {
	rec := &record{path: cardsPath, words: map[string]bool{},
		senses: map[string]bool{}}
	err := readTSV(cardsPath, func(f []string) {
		if len(f) >= 12 {
			rec.add(carded{f[1], f[2], f[3], f[4], strip(f[6]),
				f[11], f[5]})
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
	opts   []opt
	zh     int    // how many of opts, from the start, are Chinese
	notice string // stands in for opts when there are none
	sel    []bool // which of opts go on the card
	cur    int    // the option under the cursor
	top    int    // the first option in view
	dups   []carded
	src    string
	trad   bool // the text is traditional, so simplified is converted
	saving bool // the duplicate warning has been shown once
	n, of  int  // this is card n of the selection's of
}

// opt is one definition to choose for the card: one sense of it, so a
// card can carry the sense the context uses rather than all twenty of
// 打's.
type opt struct {
	py  string // the reading, with the other side's noted
	s   zhdict.Sense
	src string // the dictionary, named for a fallback's definitions
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
		for _, s := range g.Senses(i) {
			opts = append(opts, opt{py: r[2], s: s, src: src})
		}
	}
	if g.Zh == len(g.Rows) {
		zh = len(opts)
	}
	return opts, zh
}

// html is the option as it goes on the card: its part of speech, heading,
// text, examples and synonym lists on one line, and its quotations on lines
// after it when cites is set.
func (o opt) html(cites bool) string {
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
// whitespace at either end trimmed.
func (dr *draft) context() ([]rune, int, int) {
	s := dr.span
	a, b := s.bounds[s.a], s.bounds[s.b]
	for a < dr.ws && unicode.IsSpace(dr.p.text[a]) {
		a++
	}
	for b > dr.we && unicode.IsSpace(dr.p.text[b-1]) {
		b--
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
	ctx, ws, we := dr.context()
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
// generating the traditional cards. The reading carries the other side's
// only when both is set, as the pane shows it only after t, and the
// quotations go on only when cites is, as the pane shows them after x.
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
		}
		if i < dr.zh {
			zh = append(zh, o.html(cites))
		} else {
			en = append(en, o.html(cites))
		}
	}
	var trad, ctxT, clozeT string
	if f.wordT != f.wordS {
		trad = f.wordT
		ctxT = mark(f.ctxT, f.wsT, f.weT, false)
		clozeT = mark(f.ctxT, f.wsT, f.weT, true)
	}
	ctxS := mark(f.ctxS, f.wsS, f.weS, false)
	sum := sha1.Sum([]byte(f.wordS + "\x1f" + string(f.ctxS)))
	return []string{
		base64.RawURLEncoding.EncodeToString(sum[:])[:12],
		f.wordS, trad, py,
		strings.Join(zh, "<br>"), strings.Join(en, "<br>"),
		ctxS, mark(f.ctxS, f.wsS, f.weS, true), ctxT, clozeT,
		html.EscapeString(dr.src), time.Now().Format("2006-01-02"),
	}
}

// mark renders a context as HTML with the word in bold, or for the cloze
// card blanked, along with any other occurrence of it, which would
// otherwise give it away.
func mark(ctx []rune, ws, we int, cloze bool) string {
	word := string(ctx[ws:we])
	esc := func(rs []rune) string {
		s := html.EscapeString(string(rs))
		if cloze {
			s = strings.ReplaceAll(s, html.EscapeString(word),
				`<span class=cloze>[…]</span>`)
		}
		return s
	}
	mid := `<b>` + html.EscapeString(word) + `</b>`
	if cloze {
		mid = `<span class=cloze>[…]</span>`
	}
	return esc(ctx[:ws]) + mid + esc(ctx[we:])
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
	w := csv.NewWriter(fh)
	w.Comma = '\t'
	for i := range note {
		note[i] = strings.Map(func(r rune) rune {
			if r == '\t' || r == '\n' || r == '\r' {
				return ' '
			}
			return r
		}, note[i])
	}
	if err := w.Write(note); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	rec.add(carded{note[1], note[2], note[3], note[4], strip(note[6]),
		note[10], note[5]})
	return nil
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
