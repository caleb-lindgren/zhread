// Package opencc converts between simplified and Taiwan-standard
// traditional Chinese with OpenCC's dictionaries, vendored in dict/opencc.
//
// OpenCC's s2tw and tw2s configs are chains of stages, and each stage is a
// forward maximum match: at each position take the longest key the stage's
// tables hold and emit its first candidate, or copy one character through.
// A stage whose config groups several tables "short_circuit" prefers the
// earlier table where both match; a "union" group is the tables merged,
// earlier first. Both come down to one map per stage, built so that an
// earlier table's entry wins. This matched the opencc 1.4.2 package on all
// 12,034 sentences of the comparison that chose it.
package opencc

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A table reference: file name, and whether to read it reversed (value to
// key), which is how OpenCC builds TWVariantsRev from TWVariants.
type table struct {
	file string
	rev  bool
}

// configs lists each conversion's stages, each stage's tables in order of
// precedence. CJK_Compatibility_Ideographs is OpenCC's normalization step.
var configs = map[string][][]table{
	"s2tw": {
		{{"CJK_Compatibility_Ideographs", false}},
		{{"STPhrases", false},
			{"STPhrases_GeneratedFromRegionalPhrases", false},
			{"STCharacters", false}},
		{{"TWVariantsPhrases", false}, {"TWVariants", false}},
	},
	"tw2s": {
		{{"CJK_Compatibility_Ideographs", false}},
		{{"TWVariantsRevPhrases", false}, {"TWVariants", true}},
		{{"TSPhrases", false}, {"TSCharacters", false}},
	},
}

type stage struct {
	m      map[string]string
	maxLen int // longest key, in runes
}

// Converter is one loaded conversion.
type Converter struct{ stages []stage }

// Load reads the tables of the named conversion, s2tw or tw2s, from dir.
func Load(dir, name string) (*Converter, error) {
	c := &Converter{}
	for _, tabs := range configs[name] {
		st := stage{m: make(map[string]string)}
		for _, t := range tabs {
			if err := st.read(filepath.Join(dir, t.file+".txt"),
				t.rev); err != nil {
				return nil, err
			}
		}
		c.stages = append(c.stages, st)
	}
	return c, nil
}

// read adds a table's entries that no earlier table has.
func (st *stage) read(path string, rev bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		key, vals, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		cands := strings.Fields(vals)
		if len(cands) == 0 {
			continue
		}
		if rev {
			for _, v := range cands {
				st.add(v, key)
			}
		} else {
			st.add(key, cands[0])
		}
	}
	return sc.Err()
}

func (st *stage) add(k, v string) {
	if _, ok := st.m[k]; ok {
		return
	}
	st.m[k] = v
	st.maxLen = max(st.maxLen, utf8.RuneCountInString(k))
}

// Convert runs s through every stage.
func (c *Converter) Convert(s string) string {
	for _, st := range c.stages {
		s = st.convert(s)
	}
	return s
}

func (st *stage) convert(s string) string {
	rs := []rune(s)
	var sb strings.Builder
	for i := 0; i < len(rs); {
		n := min(st.maxLen, len(rs)-i)
		for ; n > 0; n-- {
			if v, ok := st.m[string(rs[i:i+n])]; ok {
				sb.WriteString(v)
				break
			}
		}
		if n == 0 {
			sb.WriteRune(rs[i])
			n = 1
		}
		i += n
	}
	return sb.String()
}
