package zhdict

import (
	"regexp"
	"strings"
)

// cell separates the parts of a definition in csld.tsv and wikt.tsv.
const cell = "¶"

// csldHead matches the start of a 兩岸詞典 sense or sub-sense: 1. or (1).
var csldHead = regexp.MustCompile(`^(?:\d+\.|\(\d+\))`)

// csldCite matches an example that is a quotation: it ends with its
// source in 《》, as 足下所以得～至今者（《史記‧卷九十二‧淮陰侯列傳》）。
var csldCite = regexp.MustCompile(`（《[^》]*》[^）]*）。?$`)

// csldMarks spells out the marks 兩岸詞典 uses for a word or sense used
// only on the Mainland (★) or in Taiwan (▲), in the style of its own
// labels 〈書〉 and 〈口〉.
var csldMarks = strings.NewReplacer("★", "〈大陸〉", "▲", "〈臺灣〉")

// CSLDSenses splits a 兩岸詞典 definition into senses. A cell ending in ：
// heads the senses after it, as Pos: a numbered one (1.擊打：) the
// sub-senses up to the next numbered cell, an unnumbered one (助詞：) all
// that follow. A cell holding only a mark labels the whole word, and goes
// in front of the first sense's Pos. Examples follow [例], split at ｜;
// those that are quotations go to Cites.
func CSLDSenses(def string) []Sense {
	var out []Sense
	head, label := "", ""
	numbered := false
	for _, c := range strings.Split(csldMarks.Replace(def), cell) {
		c = strings.TrimSpace(c)
		switch {
		case c == "":
			continue
		case c == "〈大陸〉" || c == "〈臺灣〉":
			label = c
			continue
		case strings.HasSuffix(c, "：") && !strings.Contains(c, "[例]"):
			head = c
			numbered = csldHead.MatchString(c) &&
				!strings.HasPrefix(c, "(")
			continue
		case numbered && !strings.HasPrefix(c, "("):
			head, numbered = "", false
		}
		s := Sense{Pos: head}
		text, ex, _ := strings.Cut(c, "[例]")
		s.Text = strings.TrimSpace(text)
		var exs []string
		if ex != "" {
			for _, e := range strings.Split(ex, "｜") {
				if csldCite.MatchString(e) {
					s.Cites = append(s.Cites, e)
				} else {
					exs = append(exs, e)
				}
			}
		}
		if len(exs) > 0 {
			s.Ex = []string{"[例]" + strings.Join(exs, "｜")}
		}
		out = append(out, s)
	}
	if label != "" {
		if len(out) == 0 {
			out = append(out, Sense{})
		}
		out[0].Pos = strings.TrimSpace(label + " " + out[0].Pos)
	}
	return out
}

// WiktSenses splits a Wiktionary definition into senses. The first cell
// is the part of speech, <noun>, which every sense takes as Pos; each
// sense cell is followed by its examples (例：) and quotation (引：).
func WiktSenses(def string) []Sense {
	var out []Sense
	pos := ""
	for _, c := range strings.Split(def, cell) {
		switch {
		case strings.HasPrefix(c, "<") && strings.HasSuffix(c, ">"):
			pos = c
		case strings.HasPrefix(c, "例：") || strings.HasPrefix(c, "引："):
			if len(out) == 0 {
				out = append(out, Sense{Pos: pos})
			}
			s := &out[len(out)-1]
			if c[0:len("例：")] == "例：" {
				s.Ex = append(s.Ex, c)
			} else {
				s.Cites = append(s.Cites, c)
			}
		case c != "":
			out = append(out, Sense{Pos: pos, Text: c})
		}
	}
	return out
}
