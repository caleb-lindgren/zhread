// zhread is a reader for Chinese text files that makes Anki cards of the
// words selected in it, each with the sentence it was found in.
//
// Navigation and search follow vi. The text is divided into words by the
// zhdict package's segmenter, and the definitions of the word under the
// cursor are shown below the text. Press Enter on a word, or on a
// selection made with v or V, to make a card: the preview picks the
// sentence around it as context, which [ ] { } adjust a clause at a time
// and vi's keys edit, and lists the definitions to choose from. Cards are
// appended to a card file, -cards, which is also the record checked for
// words already carded.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caleb-lindgren/zhread/internal/opencc"
	"github.com/caleb-lindgren/zhread/zhdict"
	"github.com/gdamore/tcell/v2"
)

const usage = `usage: zhread [options] FILE

Keys: hjkl w b 0 $ gg G g0 g$ gj gk, ^D ^U ^F ^B, / ? n N * #; the
arrows, Home, End, PgUp, PgDn, with Ctrl to go further and Shift to
select; v V select, Enter to make a card for the word or the selection,
:>cmd to pipe the selection to cmd; m bookmark the line, ' return to
it; s single characters, not words; e show English definitions, x
quotations, t Taiwan readings, c MoEDict, E Wiktionary; J K scroll them;
Tab move into them, Shift-Tab back, Esc back to the book; gh or F1 list
the keys; q quit.

options:
`

// convs are the two OpenCC conversions, loaded on the first card.
type convs struct{ s2t, t2s *opencc.Converter }

func main() {
	var o struct{ dict, cards, pron, deck, demo, gif string }
	var chars, pin bool
	ctl := "arrows,vi,mouse"
	flag.StringVar(&o.dict, "dict", "",
		"path to the dict directory (default:\n"+
			"$ZHREAD_DICT, else the nearest dict/ at or\n"+
			"above the executable, then the working directory)")
	flag.StringVar(&o.cards, "cards", "",
		"card file to append to (default:\n"+
			"$XDG_DATA_HOME/zhread/cards.tsv, or\n"+
			"~/.local/share/zhread/cards.tsv); with -demo, a\n"+
			"new file to save the demo's cards to")
	flag.StringVar(&o.pron, "pron", zhdict.PronCN,
		"reading to show first: cn (Mainland) or tw (Taiwan)")
	flag.BoolVar(&chars, "chars", false,
		"look up single characters rather than words, as for\n"+
			"classical Chinese (s switches while reading)")
	flag.BoolVar(&pin, "pin", false,
		"keep the cursor's line a quarter of the way down the\n"+
			"screen, scrolling the text under it, so the text\n"+
			"does not shift as the definitions change height\n"+
			"(default: the text scrolls only to keep the\n"+
			"cursor in view)")
	flag.StringVar(&ctl, "controls", ctl,
		"the ways to work zhread, any of arrows (the arrows,\n"+
			"Home, End, PgUp, PgDn and F1), vi (vi's keys) and\n"+
			"mouse, comma-separated; without mouse, the\n"+
			"terminal keeps the mouse, to select text as usual")
	flag.StringVar(&o.deck, "deck", "main",
		"Anki deck named in a new card file's header")
	flag.StringVar(&o.demo, "demo", "",
		"play the actions in this file (- for standard\n"+
			"input) and write what they show to -gif, instead\n"+
			"of reading; -demo help describes the file")
	flag.StringVar(&o.gif, "gif", "zhread.gif", "the GIF -demo writes")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if o.demo == "help" {
		fmt.Print(demoHelp)
		return
	}
	if o.demo != "" {
		if err := runDemo(o.dict, o.demo, o.gif, o.cards); err != nil {
			fail(err)
		}
		return
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if o.pron != zhdict.PronCN && o.pron != zhdict.PronTW {
		fail(fmt.Errorf("unknown -pron %q", o.pron))
	}
	cs, err := parseControls(ctl)
	if err != nil {
		fail(err)
	}

	dir, err := zhdict.FindDir(o.dict)
	if err != nil {
		fail(err)
	}
	if o.cards == "" {
		if o.cards, err = defaultCards(); err != nil {
			fail(err)
		}
	}
	dict, err := zhdict.Open(dir)
	if err != nil {
		fail(err)
	}
	if err := dict.CheckExtra(); err != nil {
		fail(err)
	}
	d, err := loadDoc(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	rec, err := loadRecord(o.cards)
	if err != nil {
		fail(err)
	}

	s, err := tcell.NewScreen()
	if err != nil {
		fail(err)
	}
	if err := s.Init(); err != nil {
		fail(err)
	}
	s.SetStyle(stDef)
	if !cs.noMouse {
		s.EnableMouse(tcell.MouseDragEvents)
	}
	reqs := make(chan segReq, 1)
	go segWorker(dict, o.pron, reqs, func(r segRes) {
		s.PostEvent(tcell.NewEventInterrupt(r))
	})
	v := &view{s: s, level: &level{d: d}, dict: dict, rec: rec, reqs: reqs,
		convDir: filepath.Join(dir, "opencc"), deck: o.deck,
		pron: o.pron, showEn: true, chars: chars, pin: pin,
		controls: cs}
	if l := d.getMark(); l >= 0 {
		v.cl = l
		v.msg = fmt.Sprintf("opened at the bookmark, line %d", l+1)
	}
	err = v.run()
	s.Fini()
	if err != nil {
		fail(err)
	}
}

// controls are the ways of working zhread that -controls leaves out, so
// that the zero value has them all. Keys that belong to neither the
// arrows nor vi, such as Enter, Space, Esc, / and the letters that toggle
// what is shown, always work, as do the arrows and vi's keys inside text
// being typed: the search prompt, and the card maker's editor.
type controls struct{ noArrows, noVi, noMouse bool }

// parseControls reads -controls: a comma-separated list of arrows, vi and
// mouse.
func parseControls(list string) (controls, error) {
	c := controls{true, true, true}
	for _, f := range strings.Split(list, ",") {
		switch strings.TrimSpace(f) {
		case "arrows":
			c.noArrows = false
		case "vi":
			c.noVi = false
		case "mouse":
			c.noMouse = false
		default:
			return c, fmt.Errorf("-controls: %q is not "+
				"arrows, vi or mouse", f)
		}
	}
	return c, nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "zhread: %v\n", err)
	os.Exit(1)
}

// defaultCards is the card file used without -cards, in the XDG data
// directory, which it creates.
func defaultCards() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("no -cards and no home directory: %w",
				err)
		}
		data = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(data, "zhread")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "cards.tsv"), nil
}
