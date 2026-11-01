package zhdict

import (
	"regexp"
	"strings"
)

// cell separates the parts of a definition in csld.tsv and wikt.tsv.
const cell = "¶"

// csldHead matches the start of a 兩岸詞典 sense or sub-sense: 1. or (1).
var csldHead = regexp.MustCompile(`^(?:\d+\.|\(\d+\))`)

// csldMarks spells out the marks 兩岸詞典 uses for a word or sense used
// only on the Mainland (★) or in Taiwan (▲), in the style of its own
// labels 〈書〉 and 〈口〉.
var csldMarks = strings.NewReplacer("★", "〈大陸〉", "▲", "〈臺灣〉")

// CSLDSenses splits a 兩岸詞典 definition into senses. A cell ending in ：
// that no examples follow heads the senses after it: a numbered one
// (1.擊打：), as Head, the sub-senses up to the next numbered cell; an
// unnumbered one (助詞：), as Pos, all that follow. A cell holding only a
// mark labels the whole word, and goes in front of the first sense's Pos.
// Each sense is followed by a cell of its examples (如：) and by cells of
// its quotations (引：), which go to Cites without the 引：.
func CSLDSenses(def string) []Sense {
	var out []Sense
	pos, head, label := "", "", ""
	cells := strings.Split(csldMarks.Replace(def), cell)
	for i, c := range cells {
		c = strings.TrimSpace(c)
		ex := strings.HasPrefix(c, "如：") || strings.HasPrefix(c, "引：")
		next := ""
		if i+1 < len(cells) {
			next = cells[i+1]
		}
		switch {
		case c == "":
			continue
		case c == "〈大陸〉" || c == "〈臺灣〉":
			label = c
			continue
		case ex:
			if len(out) == 0 {
				out = append(out, Sense{Pos: pos, Head: head})
			}
			s := &out[len(out)-1]
			if cite, ok := strings.CutPrefix(c, "引："); ok {
				s.Cites = append(s.Cites, cite)
			} else {
				s.Ex = append(s.Ex, c)
			}
			continue
		case strings.HasSuffix(c, "：") &&
			!strings.HasPrefix(next, "如：") &&
			!strings.HasPrefix(next, "引："):
			numbered := csldHead.MatchString(c) &&
				!strings.HasPrefix(c, "(")
			if numbered {
				head = c
			} else {
				pos, head = c, ""
			}
			continue
		case head != "" && !strings.HasPrefix(c, "("):
			head = ""
		}
		out = append(out, Sense{Pos: pos, Head: head, Text: c})
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
