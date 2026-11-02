package main

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/caleb-lindgren/ankitools/internal/zhdict"
	"github.com/gdamore/tcell/v2"
)

// doc is the file being read, one entry per line of the file. Lines are
// segmented into words in the background, a batch at a time around the
// view, so opening a long book costs nothing up front.
type doc struct {
	path    string
	name    string // what the status line calls it
	lines   [][]rune
	ind     [][2]int        // each line's first and wrapped columns
	sty     [][]tcell.Style // each rune's style; nil for the book
	toks    [][]tok         // nil until the line is segmented
	probe   []*zhdict.Probe // the probe that segmented the line
	pending []bool          // a request for the line is in flight
}

// tok is a word of a line: a segment of a Han run, a run of letters and
// digits, or a run of punctuation. Whitespace belongs to no word.
type tok struct {
	start, end int
	han        bool
}

func loadDoc(path string) (*doc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	d := &doc{path: path, name: filepath.Base(path)}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		d.lines = append(d.lines, []rune(l))
	}
	d.alloc()
	return d, nil
}

// paneDoc makes a definitions pane a doc, one line for each of its lines,
// each keeping its indents and colours, so it wraps as the pane did.
func paneDoc(name string, ls []styled) *doc {
	d := &doc{name: name}
	for _, l := range ls {
		sty := make([]tcell.Style, len(l.text))
		for i := range sty {
			sty[i] = l.style(i)
		}
		d.lines = append(d.lines, l.text)
		d.ind = append(d.ind, [2]int{l.x, l.wx})
		d.sty = append(d.sty, sty)
	}
	d.alloc()
	return d
}

func (d *doc) alloc() {
	n := len(d.lines)
	d.toks = make([][]tok, n)
	d.probe = make([]*zhdict.Probe, n)
	d.pending = make([]bool, n)
}

// lineToks returns a line's words, or until the line is segmented, its
// Han characters one at a time.
func (d *doc) lineToks(l int) []tok {
	if d.toks[l] != nil {
		return d.toks[l]
	}
	return tokenize(d.lines[l], nil)
}

func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// punct reports whether t, a word of line l, is a run of punctuation.
func (d *doc) punct(l int, t tok) bool {
	return !t.han && !isWord(d.lines[l][t.start])
}

// tokenize splits a line into words, segmenting Han runs with seg, or
// into characters when seg is nil or its words do not cover the run.
func tokenize(line []rune, seg func([]rune) []string) []tok {
	ts := []tok{}
	for i := 0; i < len(line); {
		r := line[i]
		j := i + 1
		switch {
		case isHan(r):
			for j < len(line) && isHan(line[j]) {
				j++
			}
			ts = append(ts, segRun(line[i:j], i, seg)...)
		case unicode.IsSpace(r):
		case isWord(r):
			for j < len(line) && isWord(line[j]) &&
				!isHan(line[j]) {
				j++
			}
			ts = append(ts, tok{i, j, false})
		default:
			for j < len(line) && !isWord(line[j]) &&
				!unicode.IsSpace(line[j]) {
				j++
			}
			ts = append(ts, tok{i, j, false})
		}
		i = j
	}
	return ts
}

func segRun(run []rune, at int, seg func([]rune) []string) []tok {
	var ts []tok
	if seg != nil {
		k := at
		for _, w := range seg(run) {
			n := utf8.RuneCountInString(w)
			ts = append(ts, tok{k, k + n, true})
			k += n
		}
		if k == at+len(run) {
			return ts
		}
	}
	ts = ts[:0]
	for k := range run {
		ts = append(ts, tok{at + k, at + k + 1, true})
	}
	return ts
}

// hanSpans are the Han runs of a line.
func hanSpans(line []rune) [][]rune {
	var out [][]rune
	for i := 0; i < len(line); {
		if !isHan(line[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(line) && isHan(line[j]) {
			j++
		}
		out = append(out, line[i:j])
		i = j
	}
	return out
}

type segReq struct {
	doc   *doc
	lines []int
	text  [][]rune
}

type segRes struct {
	doc   *doc
	lines []int
	toks  [][]tok
	probe *zhdict.Probe
}

// segWorker segments batches of lines as they are requested.
func segWorker(d *zhdict.Dict, pron string, reqs <-chan segReq,
	done func(segRes)) {
	for rq := range reqs {
		done(segment(d, pron, rq))
	}
}

// segment segments a batch with one probe: one pass over the dictionaries
// for all the batch's candidate words, which then also answers the
// definition lookups for every word in it.
func segment(d *zhdict.Dict, pron string, rq segReq) segRes {
	var runs [][]rune
	for _, l := range rq.text {
		runs = append(runs, hanSpans(l)...)
	}
	p := zhdict.NewProbe(runs)
	mode := zhdict.SegViterbi
	if len(runs) > 0 {
		p.Load(d)
		p.LoadExtra(d) // checked at startup
		if p.LoadFreq(d) != nil {
			mode = zhdict.SegBidi
		}
		p.LoadPron(d)
	}
	p.PronPref = pron
	seg := func(run []rune) []string {
		return p.Segment([][]rune{run}, mode)
	}
	res := segRes{doc: rq.doc, lines: rq.lines, probe: p}
	for _, l := range rq.text {
		res.toks = append(res.toks, tokenize(l, seg))
	}
	return res
}

func (d *doc) apply(r segRes) {
	for i, l := range r.lines {
		d.toks[l] = r.toks[i]
		d.probe[l] = r.probe
		d.pending[l] = false
	}
}

// para is a paragraph: the run of non-blank lines around a line, joined
// into one text. Hard-wrapped Chinese is joined with nothing between the
// lines; a space is put back only between two Latin letters or digits.
type para struct {
	text  []rune
	first int   // index of its first line
	offs  []int // offset in text of each of its lines
}

func blank(l []rune) bool { return strings.TrimSpace(string(l)) == "" }

func (d *doc) paragraph(l int) para {
	a, b := l, l
	for a > 0 && !blank(d.lines[a-1]) {
		a--
	}
	for b+1 < len(d.lines) && !blank(d.lines[b+1]) {
		b++
	}
	p := para{first: a}
	for i := a; i <= b; i++ {
		line := d.lines[i]
		if n := len(p.text); n > 0 && len(line) > 0 &&
			latin(p.text[n-1]) && latin(line[0]) {
			p.text = append(p.text, ' ')
		}
		p.offs = append(p.offs, len(p.text))
		p.text = append(p.text, line...)
	}
	return p
}

func latin(r rune) bool { return r < 0x80 && isWord(r) }

func (p para) pos(line, col int) int { return p.offs[line-p.first] + col }
