package zhdict

import (
	"regexp"
	"strings"
)

// Variants.
//
// A dictionary entry that is only a variant spelling of another word
// ("variant of 捲起|卷起[juǎn qǐ]", 同「暖」) says nothing the other word's
// entries do not, and listed beside them it pushes the real definitions down:
// 暖 has three CC-CEDICT entries, 煖, 㬉 and 煗, that are variants of 暖 and
// simplify to it. Rows folds such an entry into the word it points to when
// that word's entries are in the group, and the probe looks the word up when
// they are not, so that 煖 in a text shows 暖's definitions.

// cedictVariant matches a CC-CEDICT sense that names a variant's standard
// form, as "variant of 捲起|卷起[juǎn qǐ]" or "old variant of 暖[nuǎn], warm":
// the form, traditional|simplified or one for both, then the reading if
// given. A gloss after the comma repeats the standard form's.
var cedictVariant = regexp.MustCompile(
	`^(?:(?:old|archaic|ancient|obscure) )?variant of ([^\[\],;| ]+)` +
		`(?:\|([^\[\],; ]+))?(?:\[([^\]]+)\])?(?:, .*)?$`)

// zhVariant matches a Chinese definition that is only a pointer to the
// standard form, 同「咱」, after at most a gloss: 我。同「咱」。
var zhVariant = regexp.MustCompile(`^(?:[^¶｜]*。)?同「([^「」]+)」。?$`)

// variant is the standard form a variant entry points to.
type variant struct {
	trad, simp string // simp empty when the form is the same in both
	py         string // empty when the entry does not say
}

// variantOf reports what the definition def, of language lang, is a variant
// of. Every sense of an English definition has to be a variant or another
// pointer, the first a variant; a Chinese one has to be a single 同「…」.
func variantOf(def string, lang int) (variant, bool) {
	if lang == LangZh {
		m := zhVariant.FindStringSubmatch(def)
		if m == nil {
			return variant{}, false
		}
		return variant{trad: m[1]}, true
	}
	parts := strings.Split(def, "; ")
	m := cedictVariant.FindStringSubmatch(parts[0])
	if m == nil {
		return variant{}, false
	}
	for _, d := range parts[1:] {
		if !pointer.MatchString(d) {
			return variant{}, false
		}
	}
	return variant{trad: m[1], simp: m[2], py: m[3]}, true
}

// is reports whether a row with headwords trad and simp is the standard
// form v names. A form given once is matched against either headword.
func (v variant) is(trad, simp string) bool {
	if v.simp != "" {
		return v.trad == trad && v.simp == simp
	}
	return v.trad == trad || v.trad == simp
}

// sameReading compares two readings without the other side's note, case
// or spacing.
func sameReading(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(Preferred(s), " ", ""))
	}
	return norm(a) == norm(b)
}

// LoadTargets looks up the standard forms of the variant entries found,
// those not themselves among the input's candidates, so that Rows can show
// them in place of the variants. It loads the same sources the probe has,
// and the readings table if the probe has it.
func (p *Probe) LoadTargets(d *Dict) error {
	var runs [][]rune
	seen := map[string]bool{}
	for _, es := range p.hits {
		for _, e := range es {
			f := e.Fields()
			v, ok := variantOf(f[NFields-1], sources[e.src].lang)
			if !ok {
				continue
			}
			for _, w := range []string{v.trad, v.simp} {
				_, cand := p.hits[w]
				if w == "" || cand || seen[w] {
					continue
				}
				seen[w] = true
				runs = append(runs, []rune(w))
			}
		}
	}
	if len(runs) == 0 {
		return nil
	}
	q := NewProbe(runs)
	q.Load(d)
	if p.extra {
		if err := q.LoadExtra(d); err != nil {
			return err
		}
	}
	for w := range seen {
		if es := q.hits[w]; len(es) > 0 {
			p.targets[w] = es
		}
	}
	if p.pron != nil {
		return p.LoadPron(d)
	}
	return nil
}

// targetEntries are the entries of the standard forms of the variants among
// es that es does not already hold, from LoadTargets.
func (p *Probe) targetEntries(es []entry) []entry {
	have := map[*byte]bool{}
	for _, e := range es {
		have[&e.line[0]] = true
	}
	var out []entry
	for _, e := range es {
		f := e.Fields()
		v, ok := variantOf(f[NFields-1], sources[e.src].lang)
		if !ok {
			continue
		}
		for _, w := range []string{v.trad, v.simp} {
			for _, t := range p.targets[w] {
				if !have[&t.line[0]] {
					have[&t.line[0]] = true
					out = append(out, t)
				}
			}
		}
	}
	return out
}
