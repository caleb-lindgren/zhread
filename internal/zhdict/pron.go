// Taiwan and Mainland readings.
//
// MoEDict gives Taiwan readings and CC-CEDICT Mainland ones, and for some
// words they differ: 企業 is qì yè in Taiwan and qǐ yè on the Mainland. The
// cross-strait dictionary lists both readings wherever they differ, and
// csld_pron.tsv holds those rows (see dict/parse_csld.py). Each entry's
// reading is matched against the side of the table its source speaks for,
// and when a row matches, the preferred reading is printed first and the
// other noted after it: "qǐ yè (TW qì yè)".
//
// A reading only the Taiwan side lists — 和 hàn, the Taiwan reading of
// 和 hé "and" — is therefore printed as hé with hàn noted, under the
// Mainland preference, since the table says the two are the same sense.
package zhdict

import (
	"bytes"
	"regexp"
	"strings"
)

const pronPath = "csld_pron.tsv"

// Reading preference, for -pron.
const (
	PronCN = "cn"
	PronTW = "tw"
)

type pronRow struct{ tw, cn string }

// loadPron reads the rows of the table whose headwords are candidates of
// this input, keyed on the traditional form, which is what both sources'
// first field holds.
func (p *Probe) LoadPron(d *Dict) error {
	buf, err := d.pronBuf()
	if err != nil {
		return err
	}
	p.pron = make(map[string][]pronRow)
	for _, line := range bytes.Split(buf, []byte("\n")) {
		f := strings.Split(string(line), "\t")
		if len(f) != 4 || f[0] == "trad" {
			continue
		}
		_, t := p.hits[f[0]]
		_, s := p.hits[f[1]]
		if t || s {
			p.pron[f[0]] = append(p.pron[f[0]],
				pronRow{tw: f[2], cn: f[3]})
		}
	}
	return nil
}

// cedictTaiwan matches CC-CEDICT's note of a Taiwan reading when it ends
// the definition and so applies to the whole entry. A note inside
// parentheses applies to one sense and is left in the definition.
var cedictTaiwan = regexp.MustCompile(`(?:^|; )Taiwan pr\. \[([^\]]+)\]$`)

// moeLabel matches the label MoEDict puts before a reading, such as (又音)
// for an alternative one, which is kept in front of whatever is printed.
var moeLabel = regexp.MustCompile(`^\s*(\([^)]*\)\s*)?`)

// readings rewrites the pinyin field of f, from source src, to put the
// preferred reading first and note the other one. Readings the table has
// nothing for are left as the source wrote them.
func (p *Probe) readings(f *[NFields]string, src int32) {
	py := f[2]
	if py == "" {
		return
	}
	var tw, cn string
	if sources[src].lang == LangEn {
		cn = py
		if m := cedictTaiwan.FindStringSubmatchIndex(f[3]); m != nil {
			tw = f[3][m[2]:m[3]]
			f[3] = f[3][:m[0]]
		} else if r, ok := p.pronMatch(f[0], py, false); ok &&
			!p.hasReading(f[0], r.tw, LangEn) {
			tw = r.tw
		}
	} else {
		label, reading := moeReading(py)
		// The table's rows are per sense but do not say which. A
		// Taiwan reading that CC-CEDICT also gives the word is a
		// Mainland reading of some sense, so the row may be about
		// another one: MoEDict's 模 mó "model" is mó on the Mainland
		// too, and the row mó → mú is about 模 "mould". A reading
		// CC-CEDICT lacks, like 和 hàn or 企業 qì yè, is Taiwan's
		// alone, and the row is the only Mainland reading there is.
		//
		// MoEDict sometimes already gives the Mainland reading
		// (認識 rèn shi, where the table has rèn shì for Taiwan),
		// so the Mainland side is tried too, unless MoEDict has the
		// row's Taiwan reading as an entry of its own: 和 hé "sum" is
		// not the 和 hàn "and" whose Mainland reading is hé.
		if r, ok := p.pronMatch(f[0], reading, true); ok &&
			!p.hasReading(f[0], reading, LangEn) {
			tw, cn = py, label+r.cn
		} else if r, ok := p.pronMatch(f[0], reading, false); ok &&
			!p.hasReading(f[0], r.tw, LangZh) {
			tw, cn = label+r.tw, py
		} else {
			return
		}
	}
	if tw == "" || squash(tw) == squash(cn) {
		return
	}
	if p.PronPref == PronTW {
		f[2] = tw + " (CN " + cn + ")"
	} else {
		f[2] = cn + " (TW " + tw + ")"
	}
}

// otherReading matches the note of the other side's reading that readings
// adds after the preferred one.
var otherReading = regexp.MustCompile(` \((?:TW|CN) [^()]*\)$`)

// Preferred removes the note of the other side's reading from a reading
// that readings rewrote, leaving the preferred one: "qǐ yè (TW qì yè)" is
// "qǐ yè". MoEDict's own variants, as in "yī diǎnr (變)yì diǎr", stay.
func Preferred(py string) string {
	return otherReading.ReplaceAllString(py, "")
}

// moeReading splits a MoEDict pinyin field into its label and its primary
// reading. MoEDict appends variants after the primary reading, as in
// "yī diǎnr (變)yì diǎr", and only the primary is matched.
func moeReading(py string) (label, reading string) {
	label = moeLabel.FindString(py)
	reading = py[len(label):]
	if i := strings.Index(reading, " ("); i >= 0 {
		reading = reading[:i]
	}
	return label, reading
}

// hasReading reports whether an entry for word in a source of the given
// language has the given primary reading.
func (p *Probe) hasReading(word, reading string, lang int) bool {
	for _, e := range p.hits[word] {
		if sources[e.src].lang != lang {
			continue
		}
		f := e.Fields()
		if f[0] != word {
			continue
		}
		r := f[2]
		if lang == LangZh {
			_, r = moeReading(r)
		}
		if squash(r) == squash(reading) {
			return true
		}
	}
	return false
}

// pronMatch finds the row for word whose Taiwan (or Mainland) reading is
// the given one. CC-CEDICT's readings are matched against the Mainland side
// only: 和 huò is a Taiwan reading in the table, but CC-CEDICT's huò is a
// Mainland reading of another sense.
func (p *Probe) pronMatch(word, reading string, taiwan bool) (pronRow, bool) {
	want := squash(reading)
	for _, r := range p.pron[word] {
		side := r.cn
		if taiwan {
			side = r.tw
		}
		if squash(side) == want {
			return r, true
		}
	}
	return pronRow{}, false
}

// squash makes two spellings of one reading compare equal: syllable
// spacing varies between the sources. Case does not, and is kept: the
// table is all lower case, and CC-CEDICT capitalises proper nouns, so the
// surname Hé must not match the table's hé, the reading of "and".
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}
