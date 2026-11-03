package zhdict

import (
	"strings"
	"unicode"
)

// Zhuyin (bopomofo) from pinyin, for the cards' readings.

var zyInitials = []struct{ py, zy string }{
	// two-letter initials first, so zh is not read as z
	{"zh", "ㄓ"}, {"ch", "ㄔ"}, {"sh", "ㄕ"},
	{"b", "ㄅ"}, {"p", "ㄆ"}, {"m", "ㄇ"}, {"f", "ㄈ"}, {"d", "ㄉ"},
	{"t", "ㄊ"}, {"n", "ㄋ"}, {"l", "ㄌ"}, {"g", "ㄍ"}, {"k", "ㄎ"},
	{"h", "ㄏ"}, {"j", "ㄐ"}, {"q", "ㄑ"}, {"x", "ㄒ"}, {"r", "ㄖ"},
	{"z", "ㄗ"}, {"c", "ㄘ"}, {"s", "ㄙ"},
}

// zyFinals are the finals as spelled after an initial, and with y and w
// rewritten (yan as ian, wei as uei, yu as ü).
var zyFinals = map[string]string{
	"a": "ㄚ", "o": "ㄛ", "e": "ㄜ", "ê": "ㄝ", "ai": "ㄞ", "ei": "ㄟ",
	"ao": "ㄠ", "ou": "ㄡ", "an": "ㄢ", "en": "ㄣ", "ang": "ㄤ",
	"eng": "ㄥ", "ong": "ㄨㄥ", "er": "ㄦ",
	"i": "ㄧ", "ia": "ㄧㄚ", "ie": "ㄧㄝ", "iao": "ㄧㄠ", "iu": "ㄧㄡ",
	"iou": "ㄧㄡ", "ian": "ㄧㄢ", "in": "ㄧㄣ", "iang": "ㄧㄤ",
	"ing": "ㄧㄥ", "iong": "ㄩㄥ", "io": "ㄧㄛ",
	"u": "ㄨ", "ua": "ㄨㄚ", "uo": "ㄨㄛ", "uai": "ㄨㄞ", "ui": "ㄨㄟ",
	"uei": "ㄨㄟ", "uan": "ㄨㄢ", "un": "ㄨㄣ", "uen": "ㄨㄣ",
	"uang": "ㄨㄤ", "ueng": "ㄨㄥ",
	"ü": "ㄩ", "üe": "ㄩㄝ", "üan": "ㄩㄢ", "ün": "ㄩㄣ",
}

// zyTones are the tone marks, by tone; the first tone has none, and the
// neutral tone's goes before the syllable.
var zyTones = [...]string{"", "", "ˊ", "ˇ", "ˋ", "˙"}

// toned maps a vowel with a tone mark to the bare vowel and its tone.
var toned = map[rune]struct {
	v    rune
	tone int
}{}

func init() {
	for _, vs := range []string{"aāáǎà", "eēéěè", "iīíǐì", "oōóǒò",
		"uūúǔù", "üǖǘǚǜ"} {
		rs := []rune(vs)
		for t, r := range rs[1:] {
			toned[r] = struct {
				v    rune
				tone int
			}{rs[0], t + 1}
		}
	}
}

// zySyllable is one pinyin syllable, tone-marked as the dictionaries write
// them, in zhuyin, or "" if it is not one.
func zySyllable(py string) string {
	tone := 5
	var sb strings.Builder
	for _, r := range strings.ToLower(py) {
		if t, ok := toned[r]; ok {
			r, tone = t.v, t.tone
		}
		sb.WriteRune(r)
	}
	// CC-CEDICT's own notation, where the parse left it: lu:4 for lǜ.
	s := strings.NewReplacer("u:", "ü", "v", "ü").Replace(sb.String())
	if n := len(s); n > 1 && s[n-1] >= '1' && s[n-1] <= '5' {
		s, tone = s[:n-1], int(s[n-1]-'0')
	}
	if s == "r" { // an erhua written as a syllable of its own
		return "ㄦ"
	}
	er := ""
	if s != "er" && strings.HasSuffix(s, "r") {
		s, er = s[:len(s)-1], "ㄦ"
	}
	ini, fin := "", s
	for _, i := range zyInitials {
		if strings.HasPrefix(s, i.py) && len(s) > len(i.py) {
			ini, fin = i.zy, s[len(i.py):]
			break
		}
	}
	switch {
	case ini != "":
		if strings.ContainsAny(s[:1], "jqx") &&
			strings.HasPrefix(fin, "u") {
			fin = "ü" + fin[1:]
		}
		// zhi, chi, shi, ri, zi, ci, si: the initial alone
		if fin == "i" && strings.ContainsAny(s[:1], "zcsr") {
			fin = ""
		}
	case strings.HasPrefix(s, "yu"):
		fin = "ü" + s[2:]
	case s == "yi" || strings.HasPrefix(s, "yin") ||
		strings.HasPrefix(s, "ying"):
		fin = s[1:]
	case strings.HasPrefix(s, "y"):
		fin = "i" + s[1:]
	case s == "wu":
		fin = "u"
	case strings.HasPrefix(s, "w"):
		fin = "u" + s[1:]
	}
	z, ok := zyFinals[fin]
	if fin == "" && ini != "" {
		z, ok = "", true
	}
	if !ok {
		return ""
	}
	if tone == 5 {
		return zyTones[5] + ini + z + er
	}
	return ini + z + zyTones[tone] + er
}

// Zhuyin converts a reading, its syllables separated by spaces, to zhuyin,
// a space between syllables. Punctuation in it is kept. It returns "" if
// any syllable cannot be converted, rather than a reading half in each.
func Zhuyin(py string) string {
	var out []string
	for _, w := range strings.Fields(strings.ReplaceAll(py, "'", " ")) {
		if !strings.ContainsFunc(w, unicode.IsLetter) {
			out = append(out, w)
			continue
		}
		z := zySyllable(w)
		if z == "" {
			return ""
		}
		out = append(out, z)
	}
	return strings.Join(out, " ")
}
