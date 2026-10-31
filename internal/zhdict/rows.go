package zhdict

import (
	"regexp"
	"slices"
	"strings"
)

// NFields is the common field count: trad, simp, pinyin, definition.
const NFields = 4

// Group is one word's output: either its definition lines or, when no
// dictionary has the word, a notice standing in for them.
type Group struct {
	Rows   [][NFields]string
	Zh     int // how many of Rows, from the start, are Chinese
	Notice string
	// Extra marks the rows from a fallback source (tier 1) of a
	// language that a main source (tier 0) also defines the word in.
	Extra []bool
	src   []int32 // each row's source
}

// IsExtra reports Extra[i]; a Group built by hand, without Extra, has no
// extras.
func (g Group) IsExtra(i int) bool { return i < len(g.Extra) && g.Extra[i] }

// Source names the dictionary row i comes from.
func (g Group) Source(i int) string { return sources[g.src[i]].name }

// Senses splits row i's definition into senses, by its source's layout.
func (g Group) Senses(i int) []Sense {
	def := g.Rows[i][NFields-1]
	switch {
	case sources[g.src[i]].kind == kindPleco:
		return MoESenses(def)
	case sources[g.src[i]].name == "兩岸詞典":
		return CSLDSenses(def)
	case sources[g.src[i]].name == "Wiktionary":
		return WiktSenses(def)
	}
	return []Sense{{Text: def}}
}

// Line is row i for printing on one line: a 兩岸詞典 or Wiktionary
// definition's cells joined by ｜, as MoEDict's sections are, and
// 兩岸詞典's ★ and ▲ spelled out.
func (g Group) Line(i int) [NFields]string {
	r := g.Rows[i]
	if i < len(g.src) && sources[g.src[i]].kind == kindReadings {
		d := strings.ReplaceAll(csldMarks.Replace(r[NFields-1]), cell,
			" ｜ ")
		r[NFields-1] = strings.TrimPrefix(d, " ｜ ")
	}
	return r
}

// pointer matches a CC-CEDICT definition that only points elsewhere: a
// variant, a cross-reference, a classifier. A word defined only by these
// counts as undefined there, so the fallback source's entry is shown.
var pointer = regexp.MustCompile(`^(?:(?:old |archaic )?variant of .*|` +
	`see(?: also)? .*\[[^\]]*\]|CL:.*|also written .*)$`)

func pointerOnly(def string) bool {
	for _, d := range strings.Split(def, "; ") {
		if !pointer.MatchString(d) {
			return false
		}
	}
	return true
}

// Rows builds the output lines for one word: every matching entry, Chinese
// before English, main sources before fallbacks, and longest definition
// first within each. Sorting
// the two languages separately rather than the group as a whole is what
// keeps them from interleaving, since a short Chinese definition is often
// shorter than a long English one. Entries whose definitions are the same
// display width keep their load order, which is source order and then
// position in the file.
func (p *Probe) Rows(word string) Group {
	hits := p.Lookup(word)
	if len(hits) == 0 {
		return Group{Notice: noticeFor(word)}
	}
	type row struct {
		f   [NFields]string
		src int32
	}
	var g Group
	for _, lang := range []int{LangZh, LangEn} {
		var rs []row
		main := false
		for _, e := range hits {
			s := sources[e.src]
			if s.lang != lang {
				continue
			}
			f := e.Fields()
			p.readings(&f, e)
			rs = append(rs, row{f, e.src})
			if s.tier == 0 && !(lang == LangEn &&
				pointerOnly(f[NFields-1])) {
				main = true
			}
		}
		slices.SortStableFunc(rs, func(a, b row) int {
			if d := sources[a.src].tier -
				sources[b.src].tier; d != 0 {
				return d
			}
			return Width(b.f[NFields-1]) - Width(a.f[NFields-1])
		})
		for _, r := range rs {
			g.Rows = append(g.Rows, r.f)
			g.src = append(g.src, r.src)
			g.Extra = append(g.Extra, main && sources[r.src].tier > 0)
		}
		if lang == LangZh {
			g.Zh = len(g.Rows)
		}
	}
	return g
}

// noticeFor names a word that no dictionary has. It is the only line that
// repeats the input, because a word with no entry fills no column; it is
// printed flush left rather than in the columns, so it reads as a remark
// about the word rather than as a definition of it.
func noticeFor(word string) string {
	return "字典查無「" + word + "」"
}
