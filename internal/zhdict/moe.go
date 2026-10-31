package zhdict

import (
	"regexp"
	"strings"
)

// Kinds of MoELine.
const (
	MoEText    = iota // a definition with no sense number, or a note
	MoEPos            // a part of speech, as <動>
	MoENum            // a numbered sense, ① 擊、敲擊。
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
// match, and are told apart before this is tried.
var moeCite = regexp.MustCompile(`^[^「」：]{1,40}：「`)

// moePos matches a part of speech at the start of a section.
var moePos = regexp.MustCompile(`^<[^>]{1,4}>\s*`)

// MoELines splits a MoEDict definition, as Fields returns it, into its
// parts. The Pleco export separates them with a private-use mark, which
// cleanPleco turns into ｜ (MoEDict's own text never uses one), and marks
// each part-of-speech group with ◆, which is dropped: the part of speech
// after it says the same.
func MoELines(def string) []MoELine {
	var out []MoELine
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
		case strings.HasPrefix(s, "如："):
			k = MoEExample
		case strings.HasPrefix(s, "似：") ||
			strings.HasPrefix(s, "反："):
			k = MoERel
		case moeCite.MatchString(s):
			k = MoECite
		}
		out = append(out, MoELine{k, s})
	}
	return out
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

// MoESense is one sense of a MoEDict definition, with the parts that
// belong to it.
type MoESense struct {
	Pos   string // its part of speech, as <動>, or ""
	Text  string
	Ex    []string // examples, 如：「…」
	Cites []string // quotations
	Rels  []string // synonym and antonym lists
}

// MoESenses groups a definition's parts into senses: each numbered sense,
// or unnumbered definition or note, takes the examples, quotations and
// synonym lists after it, and the part of speech before it.
func MoESenses(def string) []MoESense {
	var out []MoESense
	pos := ""
	for _, l := range MoELines(def) {
		switch l.Kind {
		case MoEPos:
			pos = l.Text
			continue
		case MoEText, MoENum:
			out = append(out, MoESense{Pos: pos, Text: l.Text})
			continue
		}
		if len(out) == 0 {
			out = append(out, MoESense{Pos: pos})
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
