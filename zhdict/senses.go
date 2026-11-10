package zhdict

import (
	"regexp"
	"strings"
)

// cell separates the parts of a definition in csld.tsv and wikt.tsv.
const cell = "¶"

// csldLevels match the numbering of 兩岸詞典's senses and sub-senses, from
// the outermost: 1., (1), a).
var csldLevels = []*regexp.Regexp{
	regexp.MustCompile(`^\d+\.`),
	regexp.MustCompile(`^\(\d+\)`),
	regexp.MustCompile(`^[a-z]\)`),
}

// csldLevel is how deep a cell's numbering puts it: 1 for 1., 2 for (1), 3
// for a), and 0 for none.
func csldLevel(c string) int {
	for i, re := range csldLevels {
		if re.MatchString(c) {
			return i + 1
		}
	}
	return 0
}

// csldPos matches a heading that is a part of speech or a kind (助詞：, 擬聲詞：,
// 用於地名：) rather than a sense with sub-senses: short, unnumbered and a single
// phrase.
var csldPos = regexp.MustCompile(`^(?:〈[^〈〉]+〉)?[^\d(（。，、；]{1,6}：$`)

// csldInline matches examples the source left in a definition's text
// rather than after [例]: (3)即上、中、下三等。如：政有三品。
var csldInline = regexp.MustCompile(`。((?:例)?如：.+)$`)

// csldMarks spells out the marks 兩岸詞典 uses for a word or sense used
// only on the Mainland (★) or in Taiwan (▲), in the style of its own
// labels 〈書〉 and 〈口〉.
var csldMarks = strings.NewReplacer("★", "〈大陸〉", "▲", "〈臺灣〉")

// CSLDSenses splits a 兩岸詞典 definition into senses. A cell ending in ：
// that no examples follow heads the cells after it. A part of speech or
// kind (助詞：) is the Pos of all that follow; any other heading is one of
// the Heads of the senses numbered more deeply than it, up to the next one
// numbered as deeply: 1.擊打： over (1) and (2), and an unnumbered
// introduction (龍生九子's 依次為：) over all of them. A cell holding only a
// mark labels the whole word, and goes in front of the first sense's Pos.
// Each sense is followed by a cell of its examples (如：) and by cells of
// its quotations (引：), which go to Cites without the 引：.
func CSLDSenses(def string) []Sense {
	type heading struct {
		text  string
		level int
	}
	var out []Sense
	var stack []heading
	pos, label := "", ""
	under := func(level int) []string {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		var hs []string
		for _, h := range stack {
			hs = append(hs, h.text)
		}
		return hs
	}
	cells := strings.Split(csldMarks.Replace(def), cell)
	for i, c := range cells {
		c = strings.TrimSpace(c)
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
		case strings.HasPrefix(c, "如：") || strings.HasPrefix(c, "引："):
			if len(out) == 0 {
				out = append(out, Sense{Pos: pos})
			}
			s := &out[len(out)-1]
			if cite, ok := strings.CutPrefix(c, "引："); ok {
				s.Cites = append(s.Cites, cite)
			} else {
				s.Ex = append(s.Ex, c)
			}
			continue
		case strings.HasSuffix(c, "：") && next != "" &&
			!strings.HasPrefix(next, "如：") &&
			!strings.HasPrefix(next, "引："):
			if csldPos.MatchString(c) {
				pos, stack = c, nil
			} else {
				l := csldLevel(c)
				under(l)
				stack = append(stack, heading{c, l})
			}
			continue
		}
		s := Sense{Pos: pos, Heads: under(csldLevel(c)), Text: c}
		if m := csldInline.FindStringSubmatchIndex(c); m != nil {
			s.Text, s.Ex = c[:m[2]], []string{c[m[2]:]}
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
