// Display width of a string in terminal columns.
//
// Padding by rune count does not align CJK text: a Han character occupies
// two columns, so a column padded to a rune count comes out ragged. This is
// the East Asian Width property reduced to the ranges that actually occur in
// these dictionaries — Han, kana, Hangul, full-width punctuation and forms,
// plus the emoji planes — with combining marks counted as zero in case a
// pinyin field ever arrives decomposed.
package zhdict

type rng struct{ lo, hi rune }

var wide = []rng{
	{0x1100, 0x115F}, {0x2E80, 0x303E}, {0x3041, 0x33FF},
	{0x3400, 0x4DBF}, {0x4E00, 0x9FFF}, {0xA000, 0xA4CF},
	{0xA960, 0xA97F}, {0xAC00, 0xD7A3}, {0xF900, 0xFAFF},
	{0xFE10, 0xFE19}, {0xFE30, 0xFE6F}, {0xFF00, 0xFF60},
	{0xFFE0, 0xFFE6}, {0x1F300, 0x1F64F}, {0x1F900, 0x1F9FF},
	{0x20000, 0x2FFFD}, {0x30000, 0x3FFFD},
}

var zero = []rng{
	{0x0300, 0x036F}, {0x1AB0, 0x1AFF}, {0x1DC0, 0x1DFF},
	{0x200B, 0x200F}, {0x20D0, 0x20F0}, {0xFE00, 0xFE0F},
	{0xFE20, 0xFE2F},
}

func inRanges(r rune, rs []rng) bool {
	for _, x := range rs {
		if r >= x.lo && r <= x.hi {
			return true
		}
	}
	return false
}

func runeWidth(r rune) int {
	switch {
	case r < 0x20:
		return 0
	case r < 0x7F:
		return 1
	case inRanges(r, zero):
		return 0
	case inRanges(r, wide):
		return 2
	default:
		return 1
	}
}

func Width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// Pad appends spaces until s occupies at least w columns.
func Pad(s string, w int) string {
	n := w - Width(s)
	if n <= 0 {
		return s
	}
	return s + spaces(n)
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
