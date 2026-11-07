package main

// More contexts for a card than its book has: m in the card preview looks
// for the word in the other texts beside the book, in the Chinese
// dictionaries' entries for other words, and in sentence corpora under
// dict/corpora, and offers what it finds as further contexts, unticked,
// each labelled with where it came from.

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/caleb-lindgren/ankitools/internal/zhdict"
	"github.com/gdamore/tcell/v2"
)

const (
	// exKeep bounds the places of one source that are segmented to see
	// whether they really have the word; the shortest are kept.
	exKeep = 300
	// exBatch is how many of each source's m shows at a time.
	exBatch = 4
	// exShort is the length under which a sentence is too short to
	// recall a word from, and so comes after the longer ones.
	exShort = 8
)

// corporaDir holds the sentence corpora, one sentence a line, under dict/.
// scripts/get_corpora.sh fetches them.
const corporaDir = "corpora"

// exCand is a place found, before segmenting: the text of its sentence, or
// of the dictionary example, and where it came from.
type exCand struct {
	text  string
	label string
	ex    bool // a dictionary's example rather than its prose
}

// exSrc is one source's places, as drafts like the book's other places,
// and how many of them m has shown.
type exSrc struct {
	name   string
	found  int // places found, before segmenting
	drafts []*draft
	next   int
}

// examplesFound is the search's result, posted back from the background.
type examplesFound struct {
	dr  *draft
	src []*exSrc
}

// exWhere is what the search reads: the book's directory and path, and
// the dict directory.
type exWhere struct {
	bookDir, book, dictDir string
}

// findExamples looks for the word of f, in both scripts, in each source.
// A source comes back even when it has nothing, so the count says it was
// looked in; a source that is not there (no corpora) does not.
func findExamples(w exWhere, f forms, dict *zhdict.Dict, pron string,
	conv convs) []*exSrc {
	words := []string{f.wordS}
	if f.wordT != f.wordS {
		words = append(words, f.wordT)
	}
	var out []*exSrc
	add := func(name string, cands []exCand) {
		s := &exSrc{name: name, found: len(cands)}
		s.drafts = checkCands(cands, f, dict, pron, conv)
		out = append(out, s)
	}
	add("other texts", siblingCands(w, words))
	csld, err := os.ReadFile(filepath.Join(w.dictDir, "csld.tsv"))
	if err == nil {
		add("兩岸詞典", dictCands(csld, words, "兩岸詞典", csldEntry))
	}
	moe, err := os.ReadFile(filepath.Join(w.dictDir, "MoEDICT",
		"MoEDict-05-Simp03-cards.txt"))
	if err == nil {
		add("MoEDict", dictCands(moe, words, "MoEDict", moeEntry))
	}
	files, _ := filepath.Glob(filepath.Join(w.dictDir, corporaDir,
		"*.txt"))
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".txt")
		var cands []exCand
		eachLine(b, words, func(l []byte) {
			if i := bytes.LastIndexByte(l, '\t'); i >= 0 {
				l = l[i+1:]
			}
			cands = append(cands, exCand{text: string(l),
				label: name})
		})
		add(name, cands)
	}
	return out
}

// eachLine calls f once with each line of b that has any of words.
func eachLine(b []byte, words []string, f func([]byte)) {
	seen := map[int]bool{}
	for _, w := range words {
		wb := []byte(w)
		for at := 0; ; {
			i := bytes.Index(b[at:], wb)
			if i < 0 {
				break
			}
			i += at
			s := bytes.LastIndexByte(b[:i], '\n') + 1
			e := bytes.IndexByte(b[i:], '\n')
			if e < 0 {
				e = len(b)
			} else {
				e += i
			}
			if !seen[s] {
				seen[s] = true
				f(bytes.TrimRight(b[s:e], "\r"))
			}
			at = e
			if at >= len(b) {
				break
			}
		}
	}
}

// sentenceAt is the context zhread would start a card with for the word at
// rune offsets ws, we of text: its sentence, cut to the clauses around it
// when long.
func sentenceAt(text []rune, ws, we int) string {
	dr := &draft{p: para{text: text, offs: []int{0}}, ws: ws, we: we,
		span: newSpan(text, ws, we)}
	t, _, _ := dr.context()
	return string(t)
}

// firstAt is the rune offset of the first of words in s, and its length.
func firstAt(s string, words []string) (int, int, bool) {
	best, n := -1, 0
	for _, w := range words {
		if i := strings.Index(s, w); i >= 0 && (best < 0 || i < best) {
			best, n = i, utf8.RuneCountInString(w)
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return utf8.RuneCountInString(s[:best]), n, true
}

// siblingCands are the sentences with the word in the other .txt files of
// the book's directory, labelled with the file.
func siblingCands(w exWhere, words []string) []exCand {
	files, _ := filepath.Glob(filepath.Join(w.bookDir, "*.txt"))
	var out []exCand
	for _, p := range files {
		if p == w.book {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		name := filepath.Base(p)
		eachLine(b, words, func(l []byte) {
			s := string(l)
			rs := []rune(s)
			for _, wd := range words {
				for at := 0; ; {
					i := strings.Index(s[at:], wd)
					if i < 0 {
						break
					}
					i += at
					ws := utf8.RuneCountInString(s[:i])
					we := ws + utf8.RuneCountInString(wd)
					out = append(out, exCand{label: name,
						text: sentenceAt(rs, ws, we)})
					at = i + len(wd)
				}
			}
		})
	}
	return out
}

// An entry of a dictionary file: its headword and its definition.
type entryFn func(line string) (head, def string)

func csldEntry(l string) (string, string) {
	f := strings.Split(l, "\t")
	if len(f) < 5 {
		return "", ""
	}
	return f[0], f[len(f)-1]
}

// moeEntry reads a line of the Pleco cards, whose headword is simp[trad].
func moeEntry(l string) (string, string) {
	f := strings.SplitN(l, "\t", 3)
	if len(f) < 3 {
		return "", ""
	}
	h := f[0]
	if a := strings.IndexByte(h, '['); a >= 0 &&
		strings.HasSuffix(h, "]") {
		h = h[a+1 : len(h)-1]
	}
	return h, f[2]
}

// quoted matches an example or quotation in a definition: 「…」.
var quoted = regexp.MustCompile(`「([^「」]+)」`)

// dictCands are the examples with the word in other entries' definitions,
// or for an entry with none of its examples having it, the sentence of
// its definition that does, each labelled with the dictionary and the
// entry's headword.
func dictCands(b []byte, words []string, name string,
	entry entryFn) []exCand {
	var out []exCand
	eachLine(b, words, func(l []byte) {
		head, def := entry(string(l))
		if def == "" || slices.Contains(words, head) {
			return
		}
		label := name + "「" + head + "」"
		has := func(s string) bool {
			return slices.ContainsFunc(words, func(w string) bool {
				return strings.Contains(s, w)
			})
		}
		n := len(out)
		for _, m := range quoted.FindAllStringSubmatch(def, -1) {
			if has(m[1]) {
				out = append(out, exCand{text: m[1],
					label: label, ex: true})
			}
		}
		if len(out) > n {
			return
		}
		for _, cell := range strings.Split(def, "¶") {
			cell = numbering.ReplaceAllString(cell, "")
			// the examples after it are other words'
			if i := strings.Index(cell, "如："); i >= 0 {
				cell = cell[:i]
			}
			if ws, n, ok := firstAt(cell, words); ok {
				out = append(out, exCand{text: sentenceAt(
					[]rune(cell), ws, ws+n), label: label})
			}
		}
	})
	return out
}

// checkCands keeps the places where the segmenter takes the word as a word,
// as the book's other places are kept (a dictionary's 敲擊發出 does not
// have 擊發), each a draft for its context, shortest first, a dictionary's
// examples before its prose and sentences under exShort characters after
// the rest.
func checkCands(cands []exCand, f forms, dict *zhdict.Dict, pron string,
	conv convs) []*draft {
	rank := func(c exCand) int {
		n := utf8.RuneCountInString(c.text)
		if !c.ex {
			n += 1000
		}
		if n%1000 < exShort {
			n += 500
		}
		return n
	}
	slices.SortStableFunc(cands, func(a, b exCand) int {
		return cmp.Compare(rank(a), rank(b))
	})
	cands = cands[:min(len(cands), exKeep)]
	// In each script, the places written in it, as one doc of
	// paragraphs, which findOthers searches as it does the book.
	var out []*draft
	for _, trad := range []bool{false, true} {
		word := f.wordS
		if trad {
			word = f.wordT
		}
		d := &doc{}
		var labels []string
		for _, c := range cands {
			t := c.text
			isTrad := changed(t, conv.t2s.Convert(t)) >
				changed(t, conv.s2t.Convert(t))
			if isTrad != trad || !strings.Contains(t, word) {
				continue
			}
			if len(d.lines) > 0 {
				d.lines = append(d.lines, nil)
			}
			d.lines = append(d.lines, []rune(t))
			labels = append(labels, c.label)
		}
		if len(labels) == 0 {
			continue
		}
		d.alloc()
		w := []rune(word)
		probe := &draft{p: para{text: w, offs: []int{0}}, ws: 0,
			we: len(w), span: newSpan(w, 0, len(w)), trad: trad}
		for _, o := range findOthers(d, false, dict, pron, conv,
			probe) {
			o.from = labels[o.p.first/2]
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b *draft) int {
		return cmp.Compare(len(a.ctxf.ctxS), len(b.ctxf.ctxS))
	})
	return out
}

// moreExamples shows the next of each source's places in the preview,
// searching first, in the background, if it has not yet.
func (v *view) moreExamples() {
	dr := v.dr
	if dr.searching {
		return
	}
	if dr.exs == nil {
		dr.searching = true
		dr.exMsg = "looking for more examples…"
		book := v.d
		if len(v.stack) > 0 {
			book = v.stack[0].d
		}
		w := exWhere{bookDir: filepath.Dir(book.src), book: book.src,
			dictDir: filepath.Dir(v.convDir)}
		f := dr.forms(v.conv)
		dict, pron, conv, s := v.dict, v.pron, v.conv, v.s
		go func() {
			src := findExamples(w, f, dict, pron, conv)
			s.PostEvent(tcell.NewEventInterrupt(
				examplesFound{dr, src}))
		}()
		return
	}
	dr.showExamples()
}

// showExamples adds the next exBatch of each source's places to the
// other contexts, unticked, and says how many each has shown of how many.
func (dr *draft) showExamples() {
	var parts []string
	added := 0
	for _, s := range dr.exs {
		n := min(len(s.drafts)-s.next, exBatch)
		for _, o := range s.drafts[s.next : s.next+n] {
			dr.others = append(dr.others, o)
			dr.osel = append(dr.osel, false)
		}
		s.next += n
		added += n
		more := ""
		if s.found > exKeep {
			more = "+" // only the shortest exKeep were looked at
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d%s", s.name,
			s.next, len(s.drafts), more))
	}
	dr.exMsg = strings.Join(parts, "  ")
	if added == 0 {
		dr.exMsg = "no more examples: " + dr.exMsg
	}
}
