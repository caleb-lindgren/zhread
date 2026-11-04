// Dictionary loading and lookup.
//
// The obvious design — index every headword, then look up the input — spends
// most of its time building a map of ~465k keys, which is ~80% of the
// runtime of a single query. So the probe is inverted: the candidate
// substrings of the input (at most maxCandRunes per starting position, a few
// thousand for a sentence) go into a small map first, and the files are
// scanned once against it. That replaces 465k map inserts into a large table
// with 465k lookups in a small one, and stores only the entries that matched.
//
// Each source is read into a single []byte that is never written again.
// Matched entries hold a slice of that buffer rather than copied fields, and
// the fields are parsed out of the raw line only when they are printed.
package zhdict

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// maxCandRunes bounds the candidate substrings generated per position. The
// longest headword in the current dictionaries is 20 runes; headwords longer
// than this bound are still matched, by the fallback in Probe.offer, so the
// value only trades candidate-set size against how often that slower path
// runs.
const maxCandRunes = 24

// Definition language: Chinese definitions are printed before English ones,
// and a definition is sorted only against the others in its language.
const (
	LangZh = iota
	LangEn
)

// On-disk layout of a source file.
const (
	// kindPleco is the MoEDict Pleco card export:
	// "simp[trad]\tpinyin\tdefinition", unquoted.
	kindPleco = iota
	// kindTSV is a pandas-written TSV with a trad/simp/py/def header row.
	// pandas CSV-quotes any field containing a quote character.
	kindTSV
	// kindReadings is an unquoted TSV with a trad/simp/tw/cn/def header
	// row, carrying both readings: csld.tsv and wikt.tsv, from
	// dict/parse_csld_defs.py and dict/parse_wikt.py. Either reading is
	// empty where it is the same as the other.
	kindReadings
)

type source struct {
	path string // relative to the dict directory
	name string // shown beside a definition shown on request
	lang int
	kind int
	// tier 1 sources are fallbacks: their definitions of a word that a
	// tier 0 source of the same language defines are extras, which
	// zhread shows only on request.
	tier int
	// extra sources are read only by probes that ask for them
	// (LoadExtra): multizhdict prints what it always has.
	extra bool
}

// sources lists every dictionary. Every entry for a word is printed, and
// the language decides which half of the output an entry lands in, so this
// order only breaks ties between definitions of equal width within one
// language: each file before its additions. Add a new *_additions.tsv here.
var sources = []source{
	{filepath.Join("MoEDICT", "MoEDict-05-Simp03-cards.txt"),
		"MoE", LangZh, kindPleco, 1, false},
	{"ccdict_additions.tsv", "MoE", LangZh, kindTSV, 1, false},
	{"cedict_parsed.tsv", "CC-CEDICT", LangEn, kindTSV, 0, false},
	{"cedict_additions.tsv", "CC-CEDICT", LangEn, kindTSV, 0, false},
	{"csld.tsv", "兩岸詞典", LangZh, kindReadings, 0, true},
	{"wikt.tsv", "Wiktionary", LangEn, kindReadings, 1, true},
}

// sectionMark is the private-use character the Pleco MoEDict export uses to
// separate a definition from its citations, cross-references and synonym
// lists. It renders as a missing glyph in a terminal, so it is swapped for a
// full-width bar on output.
const sectionMark = '\uEAB1'

type entry struct {
	line []byte // raw line, a slice of the source buffer
	src  int32  // index into sources
}

// Probe holds the candidate substrings of one input and the entries found
// for them. A candidate with no entries is not a word, which is what
// segmentation needs to know.
type Probe struct {
	hits     map[string][]entry
	runs     []string // Han runs of the input, for the long-headword path
	maxRunes int      // longest headword actually matched

	// Corpus counts for the candidates, and the denominator they are
	// taken against. Both are zero until loadFreq runs, which only
	// -seg viterbi needs; see freq.go.
	freq      map[string]int64
	freqTotal int64

	// Rows of the Taiwan/Mainland reading table for the candidates, and
	// which side to print first; see pron.go. A nil map leaves every
	// reading as its source wrote it.
	pron     map[string][]pronRow
	exclude  map[string]pronRow // fallback pairs not to apply, tw/cn
	PronPref string

	// The entries of the standard forms of variants the input has, where
	// the input does not have the standard form itself; see variants.go.
	targets map[string][]entry
	extra   bool // LoadExtra has run
	// Trad says the input is traditional text, so a word matching an
	// entry's simplified headword only was written as a variant.
	Trad bool
}

// NewProbe enumerates every substring of every Han run of the input.
func NewProbe(runs [][]rune) *Probe {
	p := &Probe{hits: make(map[string][]entry),
		targets: make(map[string][]entry)}
	for _, run := range runs {
		p.runs = append(p.runs, string(run))
		for i := range run {
			n := len(run) - i
			if n > maxCandRunes {
				n = maxCandRunes
			}
			for l := 1; l <= n; l++ {
				p.hits[string(run[i:i+l])] = nil
			}
		}
	}
	return p
}

// offer records e under headword h if h is one of the candidates.
func (p *Probe) offer(h string, e entry) {
	hits, ok := p.hits[h]
	if !ok && !p.longMatch(h) {
		return
	}
	p.hits[h] = append(hits, e)
	if n := utf8.RuneCountInString(h); n > p.maxRunes {
		p.maxRunes = n
	}
}

// longMatch handles a headword longer than maxCandRunes, which was never
// enumerated as a candidate, by testing it against the input directly. The
// byte-length guard keeps that test off ordinary short headwords: a byte
// count of at most maxCandRunes bounds the rune count by the same number.
func (p *Probe) longMatch(h string) bool {
	if len(h) <= maxCandRunes {
		return false
	}
	if utf8.RuneCountInString(h) <= maxCandRunes {
		return false
	}
	return p.inInput(h)
}

func (p *Probe) inInput(h string) bool {
	for _, run := range p.runs {
		if strings.Contains(run, h) {
			return true
		}
	}
	return false
}

// has reports whether w is a word in any dictionary.
func (p *Probe) Has(w string) bool {
	return len(p.hits[w]) > 0
}

// lookup returns the entries for w in load order: source order first, then
// position within the file.
func (p *Probe) Lookup(w string) []entry {
	return p.hits[w]
}

// bs returns a string sharing b's backing array, which is what makes the
// scan allocation-free. Two conditions make that sound. The source buffers
// are read once and never written, which unsafe.String requires since Go
// strings are immutable; and the resulting string holds an interior pointer
// into the buffer, which keeps the whole buffer alive, so a string can
// outlive the last use of the probe that read it.
func bs(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}

// Load finds the candidates' entries in d, in all but the extra sources.
func (p *Probe) Load(d *Dict) {
	for si, buf := range d.bufs {
		if buf != nil {
			p.scan(int32(si), &sources[si], buf)
		}
	}
}

// LoadExtra finds the candidates' entries in the extra sources too.
func (p *Probe) LoadExtra(d *Dict) error {
	bufs, err := d.extraBufs()
	if err != nil {
		return err
	}
	p.extra = true
	for si, buf := range bufs {
		if buf != nil {
			p.scan(int32(si), &sources[si], buf)
		}
	}
	return nil
}

func (p *Probe) scan(si int32, s *source, buf []byte) {
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
		if len(line) == 0 {
			continue
		}
		trad, simp, ok := headwords(s.kind, line)
		if !ok {
			continue
		}
		e := entry{line: line, src: si}
		p.offer(trad, e)
		if simp != trad {
			p.offer(simp, e)
		}
	}
}

// headwords extracts the traditional and simplified headwords of a line.
func headwords(kind int, line []byte) (trad, simp string, ok bool) {
	switch kind {
	case kindPleco:
		t := bytes.IndexByte(line, '\t')
		if t < 0 {
			return "", "", false
		}
		head := line[:t]
		// Pleco writes characters outside Unicode as "{xxxx}"
		// placeholders; those headwords can never match input.
		if bytes.IndexByte(head, '{') >= 0 {
			return "", "", false
		}
		o := bytes.IndexByte(head, '[')
		if o < 0 || head[len(head)-1] != ']' {
			return "", "", false
		}
		return bs(head[o+1 : len(head)-1]), bs(head[:o]), true
	default:
		f1 := bytes.IndexByte(line, '\t')
		if f1 < 0 {
			return "", "", false
		}
		f2 := bytes.IndexByte(line[f1+1:], '\t')
		if f2 < 0 {
			return "", "", false
		}
		trad = unquote(line[:f1])
		simp = unquote(line[f1+1 : f1+1+f2])
		if trad == "trad" && simp == "simp" {
			return "", "", false // header row
		}
		return trad, simp, true
	}
}

// fields parses an entry into the common field order: trad, simp, pinyin,
// definition. A field a source does not carry comes back empty.
func (e entry) Fields() [NFields]string {
	var f [NFields]string
	switch sources[e.src].kind {
	case kindPleco:
		pt := splitN(e.line, '\t', 3)
		head := pt[0]
		o := bytes.IndexByte(head, '[')
		f[0] = bs(head[o+1 : len(head)-1])
		f[1] = bs(head[:o])
		f[2] = bs(pt[1])
		f[3] = cleanPleco(pt[2])
	case kindReadings:
		pt := splitN(e.line, '\t', 5)
		f[0], f[1], f[2], f[3] = bs(pt[0]), bs(pt[1]), bs(pt[2]), bs(pt[4])
	default:
		pt := splitN(e.line, '\t', 4)
		for i := range pt {
			f[i] = unquote(pt[i])
		}
	}
	return f
}

// splitN splits on sep into exactly n fields; the last field keeps any
// remaining separators, and missing fields come back nil.
func splitN(b []byte, sep byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := 0; i < n-1; i++ {
		j := bytes.IndexByte(b, sep)
		if j < 0 {
			out[i], b = b, nil
			return out
		}
		out[i], b = b[:j], b[j+1:]
	}
	out[n-1] = b
	return out
}

// unquote undoes pandas' CSV quoting, allocating only for the few hundred
// fields that are actually quoted.
func unquote(f []byte) string {
	if len(f) < 2 || f[0] != '"' || f[len(f)-1] != '"' {
		return bs(f)
	}
	return strings.ReplaceAll(string(f[1:len(f)-1]), `""`, `"`)
}

// cleanPleco makes a MoEDict definition printable on one line: runs of the
// private-use section mark become a single full-width bar, and stray control
// characters are dropped.
func cleanPleco(b []byte) string {
	s := bs(b)
	dirty := func(r rune) bool { return r == sectionMark || r < 0x20 }
	if !strings.ContainsFunc(s, dirty) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	marked := false
	for _, r := range s {
		switch {
		case r == sectionMark:
			marked = true
		case r < 0x20:
			// drop
		default:
			if marked {
				sb.WriteString(" ｜ ")
				marked = false
			}
			sb.WriteRune(r)
		}
	}
	return strings.Trim(sb.String(), " ｜")
}

// FindDir locates the dict directory: an explicit path wins, then
// $MULTIZHDICT_DICT, then the nearest dict/ at or above the executable, then
// the nearest one at or above the working directory.
func FindDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, checkDictDir(explicit)
	}
	if env := os.Getenv("MULTIZHDICT_DICT"); env != "" {
		return env, checkDictDir(env)
	}
	var starts []string
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			starts = append(starts, filepath.Dir(exe))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, start := range starts {
		for dir := start; ; {
			cand := filepath.Join(dir, "dict")
			if checkDictDir(cand) == nil {
				return cand, nil
			}
			if checkDictDir(dir) == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("cannot find the dict directory; " +
		"pass -dict DIR or set MULTIZHDICT_DICT")
}

func checkDictDir(dir string) error {
	for i := range sources {
		if sources[i].extra {
			continue
		}
		p := filepath.Join(dir, sources[i].path)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
	}
	return nil
}
