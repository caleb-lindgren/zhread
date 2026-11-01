package zhdict

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Kinds of MoELine.
const (
	MoEText    = iota // a definition with no sense number, or a note
	MoEPos            // a part of speech, as <動>
	MoENum            // a numbered sense, ① 擊、敲擊。
	MoESub            // a sub-sense of one, ⑴ 置於形容詞後。, as (1) …
	MoEExample        // 如：「打鐵趁熱」。
	MoECite           // a quotation from a book, 唐．杜甫．…：「…」
	MoERel            // synonyms (似：) or antonyms (反：)
)

// MoELine is one part of a MoEDict definition.
type MoELine struct {
	Kind int
	Text string
}

// moeCite matches the source a quotation starts with: a title, or a
// dynasty, author and title joined by ．, then ：「. Examples (如：「) also
// match, and are told apart before this is tried. A source has no 。，；,
// which tells it from a definition that quotes its source in passing:
// 山丘和谿壑。語本漢書．卷一○○．敘傳上：「…」.
var moeCite = regexp.MustCompile(`^[^「」：。，；]{1,40}：「`)

// moeInline matches a sense number that the Pleco export left inside a
// part rather than starting one: 載人 is "① 承載著人。 ② 所載的人".
var moeInline = regexp.MustCompile(`。\s*([①-⑳㉑-㉟㊱-㊿])`)

// moePos matches a part of speech at the start of a section.
var moePos = regexp.MustCompile(`^<[^>]{1,4}>\s*`)

// MoELines splits a MoEDict definition, as Fields returns it, into its
// parts. The Pleco export separates them with a private-use mark, which
// cleanPleco turns into ｜ (MoEDict's own text never uses one), and marks
// each part-of-speech group with ◆, which is dropped: the part of speech
// after it says the same.
func MoELines(def string) []MoELine {
	var out []MoELine
	def = moeInline.ReplaceAllString(def, "。｜$1")
	for _, s := range strings.Split(def, "｜") {
		s = strings.TrimSpace(strings.TrimPrefix(
			strings.TrimSpace(s), "◆"))
		if pos := moePos.FindString(s); pos != "" {
			out = append(out, MoELine{MoEPos,
				strings.TrimSpace(pos)})
			s = s[len(pos):]
		}
		if s == "" {
			continue
		}
		k := MoEText
		switch {
		case circled(s):
			k = MoENum
		case parenthesized(s):
			k = MoESub
			// As (1): the terminal may draw ⑴ two columns wide
			// and cut it off with what follows.
			r, n := utf8.DecodeRuneInString(s)
			s = fmt.Sprintf("(%d) %s", r-'⑴'+1,
				strings.TrimSpace(s[n:]))
		case strings.HasPrefix(s, "如："):
			k = MoEExample
		case strings.HasPrefix(s, "似：") ||
			strings.HasPrefix(s, "反："):
			k = MoERel
		case cites(s) != nil:
			k = MoECite
		}
		if k == MoECite || k == MoEExample || k == MoERel {
			out = append(out, MoELine{k, s})
			continue
		}
		// A definition may end with quotations: 乾食。…。詩經．小雅．
		// 伐木：「民之失德，乾餱以愆」。
		text, cs := trailingCites(s)
		out = append(out, MoELine{k, text})
		for _, c := range cs {
			out = append(out, MoELine{MoECite, c})
		}
	}
	return out
}

// cites splits s into the quotations it consists of, each a source and a
// quotation in 「」 with an optional 。 after it, or returns nil if it is
// not only quotations. An example, 如：「…」, is not one.
func cites(s string) []string {
	var out []string
	for s != "" {
		m := moeCite.FindStringIndex(s)
		if m == nil || strings.HasPrefix(s, "如：") {
			return nil
		}
		e := quoteEnd(s, m[1]-len("「"))
		if e < 0 {
			return nil
		}
		if strings.HasPrefix(s[e:], "。") {
			e += len("。")
		}
		out = append(out, s[:e])
		s = strings.TrimSpace(s[e:])
	}
	return out
}

// trailingCites splits the quotations off the end of a definition, after
// the first 。 or 」 from which the rest is only quotations.
func trailingCites(s string) (string, []string) {
	for i, r := range s {
		if r != '。' && r != '」' {
			continue
		}
		at := i + utf8.RuneLen(r)
		if cs := cites(strings.TrimSpace(s[at:])); cs != nil {
			return s[:at], cs
		}
	}
	return s, nil
}

// quoteEnd is the index just after the 」 that closes the 「 at i in s, or
// -1 if none does.
func quoteEnd(s string, i int) int {
	depth := 0
	for j, r := range s[i:] {
		switch r {
		case '「':
			depth++
		case '」':
			if depth--; depth == 0 {
				return i + j + len("」")
			}
		}
	}
	return -1
}

// circled reports whether s starts with a circled number, ① to ㊿, which
// MoEDict numbers senses with.
func circled(s string) bool {
	for _, r := range s {
		return r >= '①' && r <= '⑳' || r >= '㉑' && r <= '㉟' ||
			r >= '㊱' && r <= '㊿'
	}
	return false
}

// parenthesized reports whether s starts with a parenthesized number, ⑴
// to ⒇, which MoEDict numbers sub-senses with.
func parenthesized(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r >= '⑴' && r <= '⒇'
}

// Sense is one sense of a definition, with the parts that belong to it.
type Sense struct {
	Pos   string   // its part of speech, as <動>, or ""
	Heads []string // the senses it is a sub-sense of, outermost first
	Text  string
	Ex    []string // examples, 如：「…」
	Cites []string // quotations
	Rels  []string // synonym and antonym lists
}

// MoESenses groups a definition's parts into senses: each numbered sense,
// or unnumbered definition or note, takes the examples, quotations and
// synonym lists after it, and the part of speech before it. A numbered
// sense that sub-senses follow, as ① 結構助詞： does ⑴ to ⑷, is their
// heading rather than a sense of its own.
func MoESenses(def string) []Sense {
	var out []Sense
	pos, head := "", ""
	for _, l := range MoELines(def) {
		switch l.Kind {
		case MoEPos:
			pos, head = l.Text, ""
			continue
		case MoENum:
			head = ""
			fallthrough
		case MoEText:
			out = append(out, moeSense(pos, head, l.Text))
			continue
		case MoESub:
			if n := len(out); head == "" && n > 0 && bare(out[n-1], pos) {
				head = out[n-1].Text
				out = out[:n-1]
			}
			out = append(out, moeSense(pos, head, l.Text))
			continue
		}
		if len(out) == 0 {
			out = append(out, Sense{Pos: pos, Heads: heads(head)})
		}
		s := &out[len(out)-1]
		switch l.Kind {
		case MoEExample:
			s.Ex = append(s.Ex, l.Text)
		case MoECite:
			s.Cites = append(s.Cites, l.Text)
		case MoERel:
			s.Rels = append(s.Rels, l.Text)
		}
	}
	return out
}

// bare reports whether s is a sense under part of speech pos with only its
// text, which sub-senses after it make their heading.
func bare(s Sense, pos string) bool {
	return s.Pos == pos && len(s.Heads) == 0 &&
		len(s.Ex)+len(s.Cites)+len(s.Rels) == 0
}

func heads(head string) []string {
	if head == "" {
		return nil
	}
	return []string{head}
}

// moeSense is a sense with the example the Pleco export sometimes leaves in
// its text, as in sub-senses (⑴ 置於形容詞後。如：「美麗的風景」。), moved to Ex.
func moeSense(pos, head, text string) Sense {
	s := Sense{Pos: pos, Heads: heads(head), Text: text}
	if i := strings.Index(text, "如：「"); i > 0 {
		s.Text, s.Ex = strings.TrimSpace(text[:i]), []string{text[i:]}
	}
	return s
}
