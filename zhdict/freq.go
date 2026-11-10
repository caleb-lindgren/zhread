// Word frequencies for the unigram segmenter.
//
// Maximum matching cannot choose between two segmentations that use the same
// number of words: 交流感 splits as 交流 / 感 or 交 / 流感, each two words
// with one of them a single character, so both bidi criteria tie and the
// backward result wins by default, which is the wrong one here. Choosing
// needs a count attached to each word.
//
// A character-level table cannot supply that, and the reason is structural
// rather than a matter of coverage: both readings of 交流感 are made of 交,
// 流 and 感, so any score summed over the characters of the run is identical
// for the two. What differs between segmentations is which groupings occur,
// so the count has to be per word.
//
// The table is jieba's dict.txt, vendored at dict/jieba/dict.txt: one line
// per word, "word count POS", space separated. The counts are corpus
// occurrences; their sum over the whole file is the denominator every
// probability is taken against.
//
// jieba's headwords are simplified, while the input and two of the four
// dictionaries are usually traditional. No conversion table is needed for
// that, because every entry already carries both forms: a candidate is
// looked up under its own text and under the simplified headword of each
// entry that matched it, and the largest count found wins.
package zhdict

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
)

// freqPath is the frequency table, relative to the dict directory. It is
// not in sources: it carries no definitions, FindDir does not require
// it, and only -seg viterbi reads it.
var freqPath = filepath.Join("jieba", "dict.txt")

// loadFreq reads the counts for the words this input could contain.
//
// The file is scanned once against a map of the candidates rather than read
// into a map of its own, which is the same inversion index.go makes for the
// dictionaries: 349k inserts into a large table become 349k lookups in a
// small one, and only the counts that matter are kept.
func (p *Probe) LoadFreq(d *Dict) error {
	path := filepath.Join(d.dir, freqPath)
	buf, err := d.freqBuf()
	if err != nil {
		return fmt.Errorf("%w (needed by -seg %s; "+
			"-seg %s, %s and %s do not use it)",
			err, SegViterbi, SegBidi, SegFMM, SegBMM)
	}

	// Every candidate, plus the simplified headword of every entry found
	// for one, since that is the form the table is keyed on.
	want := make(map[string]int64, 2*len(p.hits))
	for w, hits := range p.hits {
		want[w] = 0
		for _, e := range hits {
			if _, simp, ok := entryHeadwords(e); ok {
				want[simp] = 0
			}
		}
	}

	var total int64
	for rest := buf; len(rest) > 0; {
		var line []byte
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			line, rest = rest, nil
		}
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		w, n, ok := freqLine(line)
		if !ok {
			continue
		}
		total += n
		// One word, B超, is listed twice with the same count; the
		// larger of the two readings is taken for any such pair.
		if c, seen := want[w]; seen && n > c {
			want[w] = n
		}
	}
	if total == 0 {
		return fmt.Errorf("%s: no counts in the frequency table", path)
	}

	// Fold each candidate's simplified forms into one count per
	// candidate, so the segmenter looks nothing up a second time.
	p.freq = make(map[string]int64, len(p.hits))
	for w, hits := range p.hits {
		c := want[w]
		for _, e := range hits {
			if _, simp, ok := entryHeadwords(e); ok {
				if n := want[simp]; n > c {
					c = n
				}
			}
		}
		p.freq[w] = c
	}
	p.freqTotal = total
	return nil
}

// entryHeadwords is headwords for an already-parsed entry.
func entryHeadwords(e entry) (trad, simp string, ok bool) {
	return headwords(sources[e.src].kind, e.line)
}

// freqLine parses one "word count POS" line. The part-of-speech tag is not
// used: it says what a word is, not how common it is.
func freqLine(line []byte) (word string, count int64, ok bool) {
	i := bytes.IndexByte(line, ' ')
	if i <= 0 {
		return "", 0, false
	}
	rest := line[i+1:]
	if j := bytes.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	n, err := strconv.ParseInt(bs(rest), 10, 64)
	if err != nil || n < 0 {
		return "", 0, false
	}
	return bs(line[:i]), n, true
}
