// Taiwan and Mainland readings.
//
// MoEDict gives Taiwan readings and CC-CEDICT Mainland ones, and for some
// words they differ: 企業 is qì yè in Taiwan and qǐ yè on the Mainland. The
// cross-strait dictionary lists both readings wherever they differ, and
// csld_pron.tsv holds those rows (see dict/parse_csld.py); wikt_pron.tsv
// adds Wiktionary's pairs for words it lacks, such as 導播 dǎo bò against
// dǎo bō (see dict/parse_wikt_pron.py). Each entry's
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
	"unicode/utf8"
)

// The reading tables, in the same layout. No word is in both: the
// Wiktionary table leaves out those the cross-strait one has.
var pronPaths = []string{"csld_pron.tsv", "wikt_pron.tsv"}

// excludePath lists the words fallback is not to rewrite: those whose
// MoEDict and CC-CEDICT entries were checked and found to be two senses
// rather than two standards. Columns: trad, MoEDict's reading,
// CC-CEDICT's, a note.
const excludePath = "fallback_exclude.tsv"

// Reading preference, for -pron.
const (
	PronCN = "cn"
	PronTW = "tw"
)

type pronRow struct{ tw, cn string }

// loadPron reads the rows of the tables whose headwords are candidates of
// this input, keyed on the traditional form, which is what both sources'
// first field holds.
func (p *Probe) LoadPron(d *Dict) error {
	bufs, err := d.pronBufs()
	if err != nil {
		return err
	}
	p.pron = make(map[string][]pronRow)
	for _, line := range bytes.Split(bytes.Join(bufs, []byte("\n")),
		[]byte("\n")) {
		f := strings.Split(string(line), "\t")
		if len(f) != 4 || f[0] == "trad" {
			continue
		}
		_, t := p.hits[f[0]]
		_, s := p.hits[f[1]]
		_, tt := p.targets[f[0]]
		_, ts := p.targets[f[1]]
		if t || s || tt || ts {
			p.pron[f[0]] = append(p.pron[f[0]],
				pronRow{tw: f[2], cn: f[3]})
		}
	}
	p.exclude = make(map[string]pronRow)
	for _, line := range bytes.Split(d.exclude, []byte("\n")) {
		f := strings.Split(string(line), "\t")
		if len(f) == 4 && f[0] != "trad" {
			p.exclude[f[0]] = pronRow{tw: f[1], cn: f[2]}
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
func (p *Probe) readings(f *[NFields]string, e entry) {
	src := e.src
	py := f[2]
	var tw, cn string
	switch {
	case sources[src].kind == kindReadings:
		// Both readings are in the entry; neither table is needed.
		tw, cn = py, bs(splitN(e.line, '\t', 5)[3])
		if tw == "" {
			tw = cn
		}
		if cn == "" {
			cn = tw
		}
		f[2] = cn
		if p.PronPref == PronTW {
			f[2] = tw
		}
	case py == "":
		return
	case sources[src].lang == LangEn:
		cn = py
		if m := cedictTaiwan.FindStringSubmatchIndex(f[3]); m != nil {
			tw = f[3][m[2]:m[3]]
			f[3] = f[3][:m[0]]
		} else if r, ok := p.pronMatch(f[0], py, false); ok &&
			!p.hasReading(f, r.tw, LangEn) {
			tw = r.tw
		} else if moe, ce, ok := p.fallback(f); ok &&
			strings.EqualFold(squash(ce), squash(py)) {
			tw = moe
		}
	default:
		label, reading := moeReading(py)
		// The table's rows are per sense but do not say which. A
		// Taiwan reading that CC-CEDICT also gives the word is a
		// Mainland reading of some sense, so the row may be about
		// another one: MoEDict's 模 mó "model" is mó on the Mainland
		// too, and the row mó → mú is about 模 "mold". A reading
		// CC-CEDICT lacks, like 和 hàn or 企業 qì yè, is Taiwan's
		// alone, and the row is the only Mainland reading there is.
		//
		// MoEDict sometimes already gives the Mainland reading
		// (認識 rèn shi, where the table has rèn shì for Taiwan),
		// so the Mainland side is tried too, unless MoEDict has the
		// row's Taiwan reading as an entry of its own: 和 hé "sum" is
		// not the 和 hàn "and" whose Mainland reading is hé.
		if r, ok := p.pronMatch(f[0], reading, true); ok &&
			!p.hasReading(f, reading, LangEn) {
			tw, cn = py, label+r.cn
		} else if r, ok := p.pronMatch(f[0], reading, false); ok &&
			!p.hasReading(f, r.tw, LangZh) {
			tw, cn = label+r.tw, py
		} else if moe, ce, ok := p.fallback(f); ok &&
			squash(moe) == squash(reading) {
			tw, cn = py, label+ce
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

// fallback gives the readings of a word that neither table has a row for,
// when MoEDict and CC-CEDICT each have exactly one and they differ. A row
// that applies to neither is about some other reading, or is wrong:
// Wiktionary's 薄伽丘 is bo qié qiú against bó jiā qiū, where MoEDict has
// bó qié qiū and CC-CEDICT, capitalized, Bó jiā qiū.
// MoEDict's is then taken for Taiwan's and CC-CEDICT's for the Mainland's:
// 一字不識 yī zì bù shì against yī zì bù shí, 保住 bǎo zhu against
// bǎo zhù. Most such pairs are the two standards, but not all, so the
// rule is kept to where a mismatch is least likely to be two senses:
//
//   - The word has more than one character. A single character's
//     readings are often of different senses: MoEDict's 羧 zuī is a
//     woolen cloth, CC-CEDICT's suō the carboxyl.
//   - Each dictionary has one reading. Several mean several senses, or
//     an alternative reading (MoEDict's 渴 hé, "backflowing water").
//   - The cross-strait dictionary does not have the word. Where it does
//     and has no row in its table, it gives one reading for both sides,
//     and its line is shown rather than MoEDict's.
//
// Even so, some pairs are two senses: 下咽 xià yàn "to swallow" against
// xià yān "hypopharynx", 寶子 bǎo zǐ an incense burner against bǎo zi
// "babe", and among proper nouns 南樂 nán yuè southern music against
// Nán lè the county. Those found by checking each pair by hand are in
// excludePath. Proper nouns are kept all the same, since most are
// regional readings like any other: 閩語 mín yǔ against Mǐn yǔ, 好萊塢
// hǎo lái wū against Hǎo lái wù.
//
// Whether the cross-strait dictionary has the word is known only once
// LoadExtra has run; before then there is no fallback.
func (p *Probe) fallback(w *[NFields]string) (moe, cedict string,
	ok bool) {
	word := w[0]
	if !p.extra || utf8.RuneCountInString(word) < 2 {
		return "", "", false
	}
	var nm, nc int
	for _, e := range p.entriesOf(w) {
		s := sources[e.src]
		if s.kind == kindReadings {
			head := string(splitN(e.line, '\t', 2)[0])
			if s.lang == LangZh && head == word {
				return "", "", false
			}
			continue
		}
		f := e.Fields()
		if f[0] != word || f[2] == "" {
			continue
		}
		switch s.lang {
		case LangZh:
			_, r := moeReading(f[2])
			if nm == 0 {
				moe = r
			}
			if squash(r) != squash(moe) {
				return "", "", false
			}
			nm++
		case LangEn:
			if cedictTaiwan.MatchString(f[3]) {
				return "", "", false
			}
			// A name and a common noun can share a reading, in
			// different case (名利場 Míng lì chǎng and míng lì
			// chǎng); the lower-case spelling is kept.
			if nc > 0 && !strings.EqualFold(squash(f[2]),
				squash(cedict)) {
				return "", "", false
			}
			if nc == 0 || f[2] == strings.ToLower(f[2]) {
				cedict = f[2]
			}
			nc++
		}
	}
	if nm == 0 || nc == 0 ||
		strings.EqualFold(squash(moe), squash(cedict)) {
		return "", "", false
	}
	// An exclusion holds while both dictionaries give the readings it
	// was checked against.
	if x, ok := p.exclude[word]; ok && squash(x.tw) == squash(moe) &&
		squash(x.cn) == squash(cedict) {
		return "", "", false
	}
	// A row is in the way only if readings would apply it: to MoEDict's
	// reading on either side, to CC-CEDICT's on the Mainland side, in
	// the same case, as pronMatch compares.
	for _, r := range p.pron[word] {
		m, t, c := squash(moe), squash(r.tw), squash(r.cn)
		if m == t || m == c || squash(cedict) == c {
			return "", "", false
		}
	}
	return moe, cedict, true
}

// entriesOf gives the entries that may be of the word whose fields f
// are. The candidates are keyed on the form the input has, which may be
// either headword: 一字不识 finds the entries of 一字不識 under its
// simplified form. Callers still check the traditional headword.
func (p *Probe) entriesOf(f *[NFields]string) []entry {
	var es []entry
	for i, k := range []string{f[0], f[1]} {
		if i == 1 && k == f[0] {
			break
		}
		if hs := p.hits[k]; len(hs) > 0 {
			es = append(es, hs...)
		} else {
			es = append(es, p.targets[k]...)
		}
	}
	return es
}

// hasReading reports whether an entry in a source of the given language
// for the word whose fields f are has the given primary reading.
func (p *Probe) hasReading(f *[NFields]string, reading string,
	lang int) bool {
	word := f[0]
	for _, e := range p.entriesOf(f) {
		if sources[e.src].lang != lang ||
			sources[e.src].kind == kindReadings {
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
// table is all lower case, and CC-CEDICT capitalizes proper nouns, so the
// surname Hé must not match the table's hé, the reading of "and".
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}
