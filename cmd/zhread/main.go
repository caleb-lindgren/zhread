// zhread is a reader for Chinese text files that makes Anki cards of the
// words selected in it, each with the sentence it was found in.
//
// Navigation and search follow vi. The words of the text come from the
// same segmenter as multizhdict, and the definitions of the word under the
// cursor are shown below the text. Select a word with v or V and press
// Enter to make a card: the preview picks the sentence around it as
// context, which [ ] { } adjust a clause at a time, and lists the
// definitions to choose from. Cards are appended to cards.tsv, which is
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

Keys: hjkl w b 0 $ gg G g0 g$ gj gk, ^D ^U ^F ^B, / n N * #;
v V select, Enter to make a card of the selection, :>cmd to pipe it
to cmd; m bookmark the line, ' return to it; e show English
definitions; t show Taiwan readings; ? list the keys; q quit.

options:
`

// convs are the two OpenCC conversions, loaded on the first card.
type convs struct{ s2t, t2s *opencc.Converter }

func main() {
	var o struct{ dict, cards, pron, deck string }
	flag.StringVar(&o.dict, "dict", "",
		"path to the dict directory (default: found as\n"+
			"multizhdict finds it)")
	flag.StringVar(&o.cards, "cards", "",
		"card file to append to (default: cards.tsv beside dict/)")
	flag.StringVar(&o.pron, "pron", zhdict.PronCN,
		"reading to show first: cn (Mainland) or tw (Taiwan)")
	flag.StringVar(&o.deck, "deck", "Chinese reading",
		"Anki deck named in a new card file's header")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
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
	reqs := make(chan segReq, 1)
	go segWorker(dict, o.pron, reqs, func(r segRes) {
		s.PostEvent(tcell.NewEventInterrupt(r))
	})
	v := &view{s: s, d: d, dict: dict, rec: rec, reqs: reqs,
		convDir: filepath.Join(dir, "opencc"), deck: o.deck,
		pron: o.pron}
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
