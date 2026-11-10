// Word segmentation.
//
// Four segmenters, selected by -seg. The default, viterbi, scores competing
// segmentations with a unigram frequency model; the other three are maximum
// matching, which has no scores and so can only count words.
//
// Forward maximum matching alone errs in a predictable direction: wherever a
// long word and a shorter one share a prefix it always takes the long one, so
// a boundary that should have fallen inside the long candidate is lost.
// 研究生命起源 comes out as 研究生 / 命 / 起源 rather than 研究 / 生命 / 起源.
// The same greedy match run from the right makes a different set of mistakes,
// and the two agree except where the run is genuinely ambiguous, which is
// exactly where a tie-break is worth applying. Hence bidi, which was the
// default before there were frequencies to score with.
//
// Where bidi's two criteria both tie it is left choosing arbitrarily, and
// arbitrarily is sometimes wrong: 交流感 is 交流 / 感 forwards and 交 / 流感
// backwards, two words either way and one single character either way, and
// the backward result wins only because it is the fallback. viterbi resolves
// that case on the counts, which say 流感 is rare and 交流 common. See
// freq.go for where the counts come from and why they have to be per word.
package zhdict

import (
	"math"
	"strings"
	"unicode"
)

// Segmentation modes, as accepted by -seg.
const (
	SegFMM     = "fmm"
	SegBMM     = "bmm"
	SegBidi    = "bidi"
	SegViterbi = "viterbi"
	// SegChar makes every character a word of its own, for classical
	// Chinese, where most words are one character and a segmenter
	// trained on modern text joins them (非常道 as 非常 / 道).
	SegChar = "char"
)

// joiners are the non-Han runes that occur inside headwords, so a run has to
// be allowed to span them. 1,469 entries across MoEDict and CC-CEDICT have a
// full-width comma in a headword, as in 不是東風壓了西風，就是西風壓了東風, and 197 have an
// interpunct, as in 卡爾·馬克思. Every other rune ends a run: spaces, Latin,
// digits, and sentence-ending punctuation.
const joiners = "，·．；○"

func isJoiner(r rune) bool {
	return strings.ContainsRune(joiners, r)
}

// HanRuns splits s into maximal runs of Han characters and joiners. A run
// with no Han character in it is dropped.
func HanRuns(s string) [][]rune {
	var runs [][]rune
	var cur []rune
	han := false
	flush := func() {
		for len(cur) > 0 && isJoiner(cur[len(cur)-1]) {
			cur = cur[:len(cur)-1]
		}
		if han {
			runs = append(runs, cur)
		}
		cur, han = nil, false
	}
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			cur = append(cur, r)
			han = true
		case isJoiner(r) && len(cur) > 0:
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return runs
}

// segment splits each Han run into words. Mode SegViterbi requires that
// Probe.LoadFreq has run, which is what main does when -seg asks for it.
func (p *Probe) Segment(runs [][]rune, mode string) []string {
	var out []string
	for _, run := range runs {
		switch mode {
		case SegFMM:
			out = append(out, p.fmm(run)...)
		case SegBMM:
			out = append(out, p.bmm(run)...)
		case SegBidi:
			out = append(out, pickBidi(p.fmm(run), p.bmm(run))...)
		case SegChar:
			for _, r := range run {
				out = append(out, string(r))
			}
		default:
			out = append(out, p.viterbi(run)...)
		}
	}
	return out
}

// unknownCount is the count given to a word the table does not list. One
// occurrence is below the smallest count in the file, which is 2, so an
// unlisted word is always rarer than a listed one but is still a word.
//
// That floor is what keeps a word only the additions files carry from being
// split into its characters. 露鋒, 方筆 and 館閣體 are listed in none of the
// corpora the table was built from, so each scores log(1/total) = -17.91;
// the characters they are made of are common enough to cost more, 露 / 鋒
// scoring -9.97 + -11.12 = -21.09. A lower floor inverts that and shatters
// the word, which is the check to run if this value is ever changed.
const unknownCount = 1

// viterbi returns the segmentation with the highest total log probability
// under a unigram model: every word is scored as count/total independently
// of its neighbors, and the best path through the lattice of dictionary
// words is taken. Because each word contributes -log(total) on top of its
// own count, the model penalizes using more words, which is bidi's first
// criterion arrived at from the frequencies rather than asserted.
//
// The recurrence runs from the right: best[i] is the score of the best
// segmentation of rs[i:], so best[0] is the answer and pick[i] records the
// word length that achieved it. Candidates are tried longest first and the
// comparison is strict, so a tie keeps the longer word.
func (p *Probe) viterbi(rs []rune) []string {
	n := len(rs)
	logTotal := math.Log(float64(p.freqTotal))
	best := make([]float64, n+1)
	pick := make([]int, n+1)
	for i := n - 1; i >= 0; i-- {
		best[i] = math.Inf(-1)
		for l := p.maxTry(n - i); l >= 1; l-- {
			w := string(rs[i : i+l])
			if l > 1 && !p.Has(w) {
				continue
			}
			var sc float64
			if l == 1 && isJoiner(rs[i]) {
				// Not a word: crossing one is free, and it
				// is dropped from the output below. Scoring
				// it like a word would make the model shy
				// away from splitting at a comma.
				sc = best[i+1]
			} else {
				c := p.freq[w]
				if c < unknownCount {
					c = unknownCount
				}
				sc = math.Log(float64(c)) - logTotal +
					best[i+l]
			}
			if sc > best[i] {
				best[i], pick[i] = sc, l
			}
		}
	}
	var out []string
	for i := 0; i < n; {
		l := pick[i]
		if l > 1 || !isJoiner(rs[i]) {
			out = append(out, string(rs[i:i+l]))
		}
		i += l
	}
	return out
}

// fmm takes the longest known word starting at each position, falling back
// to a single character where no longer word is known.
func (p *Probe) fmm(rs []rune) []string {
	var out []string
	for i := 0; i < len(rs); {
		n := p.maxTry(len(rs) - i)
		for ; n > 1; n-- {
			if p.Has(string(rs[i : i+n])) {
				break
			}
		}
		if n == 1 && isJoiner(rs[i]) {
			i++ // a joiner that no word covers is not a word
			continue
		}
		out = append(out, string(rs[i:i+n]))
		i += n
	}
	return out
}

// bmm is fmm run from the end of the string.
func (p *Probe) bmm(rs []rune) []string {
	var out []string
	for j := len(rs); j > 0; {
		n := p.maxTry(j)
		for ; n > 1; n-- {
			if p.Has(string(rs[j-n : j])) {
				break
			}
		}
		if n == 1 && isJoiner(rs[j-1]) {
			j--
			continue
		}
		out = append(out, string(rs[j-n:j]))
		j -= n
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// maxTry is the longest match worth attempting: no candidate longer than the
// longest headword that matched anything can succeed.
//
// One character is always worth trying, even when nothing matched at all and
// maxRunes is therefore 0. A character no dictionary has is still emitted as
// a word of its own, and without this floor every segmenter would take a
// zero-length word and never advance past it.
func (p *Probe) maxTry(avail int) int {
	n := p.maxRunes
	if avail < n {
		n = avail
	}
	if n < 1 {
		n = 1
	}
	return n
}

// pickBidi chooses between the two maximum-matching results: prefer fewer
// words, then fewer single-character words, then the backward result, which
// is the conventional default for this heuristic. Both criteria favor the
// segmentation that accounts for the run with more multi-character
// dictionary words and fewer leftover characters.
func pickBidi(fwd, bwd []string) []string {
	if len(fwd) != len(bwd) {
		if len(fwd) < len(bwd) {
			return fwd
		}
		return bwd
	}
	if sf, sb := singles(fwd), singles(bwd); sf != sb {
		if sf < sb {
			return fwd
		}
		return bwd
	}
	return bwd
}

func singles(words []string) int {
	n := 0
	for _, w := range words {
		if len([]rune(w)) == 1 {
			n++
		}
	}
	return n
}
