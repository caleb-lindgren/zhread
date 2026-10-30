package zhdict

import "slices"

// NFields is the common field count: trad, simp, pinyin, definition.
const NFields = 4

// Group is one word's output: either its definition lines or, when no
// dictionary has the word, a notice standing in for them.
type Group struct {
	Rows   [][NFields]string
	Notice string
}

// Rows builds the output lines for one word: every matching entry, Chinese
// before English and longest definition first within each language. Sorting
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
	rows := make([][NFields]string, 0, len(hits))
	for _, lang := range []int{LangZh, LangEn} {
		start := len(rows)
		for _, e := range hits {
			if sources[e.src].lang == lang {
				f := e.Fields()
				p.readings(&f, e.src)
				rows = append(rows, f)
			}
		}
		slices.SortStableFunc(rows[start:],
			func(a, b [NFields]string) int {
				return Width(b[NFields-1]) -
					Width(a[NFields-1])
			})
	}
	return Group{Rows: rows}
}

// noticeFor names a word that no dictionary has. It is the only line that
// repeats the input, because a word with no entry fills no column; it is
// printed flush left rather than in the columns, so it reads as a remark
// about the word rather than as a definition of it.
func noticeFor(word string) string {
	return "字典查無「" + word + "」"
}
