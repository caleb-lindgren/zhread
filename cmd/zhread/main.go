// zhread is a reader for Chinese text files that makes Anki cards of the
// words selected in it, each with the sentence it was found in.
//
// Navigation and search follow vi. The words of the text come from the
// same segmenter as multizhdict, and the definitions of the word under the
// cursor are shown below the text. Press Enter on a word, or on a selection
// made with v or V, to make a card: the preview picks the sentence around
// it as context, which [ ] { } adjust a clause at a time and vi's keys
// edit, and lists the definitions to choose from. Cards are appended to cards.tsv, which is
// also the record checked for words already carded.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caleb-lindgren/ankitools/internal/opencc"
	"github.com/caleb-lindgren/ankitools/internal/zhdict"
	"github.com/gdamore/tcell/v2"
)

const usage = `usage: zhread [options] FILE

Keys: hjkl w b 0 $ gg G g0 g$ gj gk, ^D ^U ^F ^B, / ? n N * #; the
arrows, Home, End, PgUp, PgDn, with Ctrl to go further and Shift to
select; v V select, Enter to make a card of the word or the selection,
:>cmd to pipe the selection to cmd; m bookmark the line, ' return to
it; e show English definitions, x quotations, t Taiwan readings, c
MoEDict, E Wiktionary; J K scroll them; Tab move into them, Shift-Tab
back, Esc back to the book; gh or F1 list the keys; q quit.

options:
`

// convs are the two OpenCC conversions, loaded on the first card.
type convs struct{ s2t, t2s *opencc.Converter }

func main() {
	var o struct{ dict, cards, pron, deck, demo, gif string }
	flag.StringVar(&o.dict, "dict", "",
		"path to the dict directory (default: found as\n"+
			"multizhdict finds it)")
	flag.StringVar(&o.cards, "cards", "",
		"card file to append to (default: cards.tsv beside dict/)")
	flag.StringVar(&o.pron, "pron", zhdict.PronCN,
		"reading to show first: cn (Mainland) or tw (Taiwan)")
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
		if err := runDemo(o.dict, o.demo, o.gif); err != nil {
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

	dir, err := zhdict.FindDir(o.dict)
	if err != nil {
		fail(err)
	}
	repo := filepath.Dir(filepath.Clean(dir))
	if o.cards == "" {
		o.cards = filepath.Join(repo, "cards.tsv")
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
	s.EnableMouse(tcell.MouseDragEvents)
	reqs := make(chan segReq, 1)
	go segWorker(dict, o.pron, reqs, func(r segRes) {
		s.PostEvent(tcell.NewEventInterrupt(r))
	})
	v := &view{s: s, level: &level{d: d}, dict: dict, rec: rec, reqs: reqs,
		convDir: filepath.Join(dir, "opencc"), deck: o.deck,
		pron: o.pron, showEn: true}
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

func fail(err error) {
	fmt.Fprintf(os.Stderr, "zhread: %v\n", err)
	os.Exit(1)
}
