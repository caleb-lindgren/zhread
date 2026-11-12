package main

// Editing a card already made: u in the preview of a new card for a word
// already carded opens that card in the same preview, filled in from its
// row of cards.tsv, and Enter saves it back over the row, keeping its GUID,
// so that importing the file again updates the note in Anki.

import (
	"cmp"
	"errors"
	"html"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/caleb-lindgren/zhread/zhdict"
)

// row is the row of cards.tsv with the GUID guid, padded to every column.
func (rec *record) row(guid string) ([]string, error) {
	var out []string
	err := readTSV(rec.path, func(f []string) {
		if out == nil && len(f) > 0 && f[0] == guid {
			out = append([]string(nil), f...)
		}
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("no card " + guid + " in " + rec.path)
	}
	for len(out) < 1+len(fields) {
		out = append(out, "")
	}
	return out, nil
}

// zhuyinPart matches the zhuyin withZhuyin puts after a reading.
var zhuyinPart = regexp.MustCompile(`　[\p{Bopomofo}ˊˇˋ˙ ]*`)

// ctxDraft makes a context of a card, as HTML with the word in bold, a
// draft of its own, the whole context its span. The word is the first
// place in bold that has word, or failing that the first place word is.
func ctxDraft(h, word string, trad bool) *draft {
	text := []rune(strip(h))
	w := []rune(word)
	ws := -1
	for at := 0; ; {
		i := strings.Index(h[at:], "<b>")
		if i < 0 {
			break
		}
		c := utf8.RuneCountInString(strip(h[:at+i]))
		if c+len(w) <= len(text) &&
			slices.Equal(text[c:c+len(w)], w) {
			ws = c
			break
		}
		at += i + 3
	}
	if ws < 0 {
		ws = 0
		for i := 0; i+len(w) <= len(text); i++ {
			if slices.Equal(text[i:i+len(w)], w) {
				ws = i
				break
			}
		}
	}
	we := min(ws+len(w), len(text))
	bounds, sentEnd := clauses(text)
	sp := spanIn(bounds, sentEnd, ws, we)
	sp.a, sp.b = 0, sp.n()
	return &draft{p: para{text: text, offs: []int{0}}, ws: ws, we: we,
		span: sp, trad: trad}
}

// editCard opens card k in the preview in place of the draft dr, a new card
// for the same word: its definitions ticked as the card has them, any no
// dictionary shown has kept as they are, its own context, then its other
// contexts in their order, ticked, then last dr's context, ticked, unless
// the card has it already. So each new card for a word that e adds to its
// card instead adds one context to it, keeping those before. Every key of
// the preview works on it as on a new card.
func (v *view) editCard(dr *draft, k carded) error {
	row, err := v.rec.row(k.guid)
	if err != nil {
		return err
	}
	// The card's contexts in the script of its own: the one it has, or
	// with both, the book's.
	trad := dr.trad
	switch {
	case row[6] == "":
		trad = true
	case row[8] == "":
		trad = false
	}
	ctx, more, word := row[6], row[12], row[1]
	if trad {
		ctx, more, word = row[8], row[13], cmp.Or(row[2], row[1])
	}
	ed := ctxDraft(ctx, word, trad)
	ed.g, ed.notice = dr.g, dr.notice
	ed.src = html.UnescapeString(row[10])
	ed.guid, ed.added = k.guid, row[11]
	ed.n, ed.of = dr.n, dr.of
	ed.cur = 0

	// The definitions: the card's ticked where a dictionary has them,
	// MoEDict's and Wiktionary's shown if the card has theirs, its
	// quotations shown if it has any, and the rest kept as they are.
	py := zhuyinPart.ReplaceAllString(row[3], "")
	py = strings.ReplaceAll(py, "  ", " ")
	if strings.Contains(py, "(TW ") || strings.Contains(py, "(CN ") {
		v.showTW = true
	}
	type def struct {
		html string
		en   bool
	}
	var defs []def
	for _, c := range []int{4, 5} {
		for _, h := range strings.Split(row[c], "<br>") {
			if h != "" {
				defs = append(defs, def{h, c == 5})
			}
		}
	}
	every := func(zhdict.Group, int) bool { return true }
	ed.setOpts(every)
	var ticks []int
	for i := 0; i < len(defs); i++ {
		at := slices.IndexFunc(ed.opts, func(o opt) bool {
			return o.html(false) == defs[i].html
		})
		if at < 0 {
			row := -1
			if defs[i].en {
				row = -2
			}
			ed.raws = append(ed.raws, opt{py: py, row: row,
				s:   zhdict.Sense{Text: strip(defs[i].html)},
				src: "on the card", k: len(ed.raws),
				raw: defs[i].html})
			continue
		}
		ticks = append(ticks, at)
		o := ed.opts[at]
		switch {
		case o.src != "" && at < ed.zh:
			v.moreZh = true
		case o.src != "":
			v.moreEn = true
		}
		// its quotations follow it
		for _, c := range o.s.Cites {
			if i+1 < len(defs) &&
				defs[i+1].html == html.EscapeString(c) {
				i++
				v.expand = true
			}
		}
	}
	// ticks are into the options with every dictionary shown; with
	// those ticked, the card's own are added, ticked too, and then the
	// options are those the toggles now show
	for _, i := range ticks {
		ed.sel[i] = true
	}
	ed.setOpts(every)
	for i := range ed.opts {
		if ed.opts[i].raw != "" {
			ed.sel[i] = true
		}
	}
	ed.setOpts(v.shows)

	// The contexts.
	for _, m := range divs.FindAllStringSubmatch(more, -1) {
		o := ctxDraft(m[0][len("<div>"):len(m[0])-len("</div>")], word,
			trad)
		ed.others = append(ed.others, o)
		ed.osel = append(ed.osel, true)
	}
	have := map[string]bool{}
	own := func(o *draft) string {
		t, _, _ := o.context()
		return string(t)
	}
	have[own(ed)] = true
	for _, o := range ed.others {
		have[own(o)] = true
	}
	ed.added = row[11]
	add := func(o *draft, tick bool) {
		o.refresh(v.conv)
		t := string(o.ctxf.ctxS)
		if trad {
			t = string(o.ctxf.ctxT)
		}
		if have[t] {
			return
		}
		have[t] = true
		ed.others = append(ed.others, o)
		ed.osel = append(ed.osel, tick)
	}
	for _, o := range ed.others {
		o.refresh(v.conv)
	}
	add(&draft{p: dr.p, ws: dr.ws, we: dr.we, span: dr.span,
		trad: dr.trad, ed: dr.ed}, true)
	v.dr = ed
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
