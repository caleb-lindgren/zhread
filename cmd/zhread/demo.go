package main

// The demo: gozhread -demo ACTIONS -gif OUT plays a list of actions on a
// simulated screen and writes what the screen showed after each as a frame
// of an animated GIF. scripts/zhread_demo.sh holds the actions the README's
// GIF shows. Everything is drawn here, glyph by glyph, from a font file,
// so nothing is needed beyond Go and a font with Chinese in it.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/caleb-lindgren/zhread/zhdict"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// demoHelp documents the actions file, for -demo with -h.
const demoHelp = `An actions file has one action a line; # starts a comment.

  size COLS ROWS   the terminal's size (default 90 26); before open
  font PX [FILE]   the font's pixel size (default 20), and its file
                   (default: fc-match's monospace for zh-cn)
  open FILE        the text to read
  find TEXT        put the cursor on TEXT's first place
  line N           put the cursor on line N
  delay MS         how long each frame after it shows (default 500)
  caption TEXT     shown from now on in a banner above the terminal, in
                   a proportional font so it reads as the demo's and not
                   zhread's; none clears it
  banner PX [FILE] the banner font's pixel size (default 22), and its
                   file (default: fc-match's sans-serif for zh-cn)
  keys KEY...      press each key, a frame each: a character as itself,
                   or <Name> with Name one of Enter Esc Tab S-Tab Space
                   BS Left Right Up Down Home End PgUp PgDn F1, after
                   any of C- S- M- (<C-S-Right>); KEY*N presses it N times
  click X Y        press the left button at column X, row Y, and let go
  drag X Y X2 Y2   press at X Y, move to X2 Y2 a column at a time, let go
  pause MS         show the last frame MS longer
  forget           forget the cards made so far, unseen, so the same
                   word can be carded again without the warning
`

// demoKey is a key of a keys action, and how the caption names it.
type demoKey struct {
	ev    *tcell.EventKey
	label string
}

var demoKeys = map[string]struct {
	k     tcell.Key
	label string
}{
	"Enter": {tcell.KeyEnter, "Enter"}, "Esc": {tcell.KeyEscape, "Esc"},
	"Tab": {tcell.KeyTab, "Tab"}, "S-Tab": {tcell.KeyBacktab, "Shift+Tab"},
	"BS": {tcell.KeyBackspace2, "Backspace"}, "Left": {tcell.KeyLeft, "←"},
	"Right": {tcell.KeyRight, "→"}, "Up": {tcell.KeyUp, "↑"},
	"Down": {tcell.KeyDown, "↓"}, "Home": {tcell.KeyHome, "Home"},
	"End": {tcell.KeyEnd, "End"}, "PgUp": {tcell.KeyPgUp, "PgUp"},
	"PgDn": {tcell.KeyPgDn, "PgDn"}, "F1": {tcell.KeyF1, "F1"},
	"Space": {tcell.KeyRune, "Space"},
}

// parseKeys reads the keys of a keys action.
func parseKeys(args string) ([]demoKey, error) {
	var out []demoKey
	rs := []rune(args)
	for i := 0; i < len(rs); {
		var k demoKey
		if rs[i] == ' ' {
			i++
			continue
		}
		if rs[i] == '<' {
			j := i + 1
			for j < len(rs) && rs[j] != '>' {
				j++
			}
			if j == len(rs) {
				return nil, fmt.Errorf("unclosed <: %s", args)
			}
			name := string(rs[i+1 : j])
			var mod tcell.ModMask
			var label string
			for {
				switch {
				case strings.HasPrefix(name, "C-"):
					mod |= tcell.ModCtrl
					label += "Ctrl+"
				case strings.HasPrefix(name, "S-") &&
					name != "S-Tab":
					mod |= tcell.ModShift
					label += "Shift+"
				case strings.HasPrefix(name, "M-"):
					mod |= tcell.ModAlt
					label += "Alt+"
				default:
					goto named
				}
				name = name[2:]
			}
		named:
			d, ok := demoKeys[name]
			if !ok {
				return nil, fmt.Errorf("unknown key <%s>",
					name)
			}
			r := rune(0)
			if name == "Space" {
				r = ' '
			}
			k = demoKey{tcell.NewEventKey(d.k, r, mod),
				label + d.label}
			i = j + 1
		} else {
			k = demoKey{tcell.NewEventKey(tcell.KeyRune, rs[i],
				tcell.ModNone), string(rs[i])}
			i++
		}
		n := 1
		if i < len(rs) && rs[i] == '*' && k.ev.Key() != tcell.KeyRune {
			j := i + 1
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			var err error
			n, err = strconv.Atoi(string(rs[i+1 : j]))
			if err != nil {
				return nil, fmt.Errorf("bad count in %s", args)
			}
			i = j
		}
		for ; n > 0; n-- {
			out = append(out, k)
		}
	}
	return out, nil
}

// frame is the screen after an action, and how long it shows.
type frame struct {
	cells   []tcell.SimCell
	w, h    int
	cx, cy  int // the cursor, if shown
	cursor  bool
	caption string
	keys    string // the key just pressed
	ms      int
}

// demo plays the actions on a simulated screen.
type demo struct {
	v       *view
	s       tcell.SimulationScreen
	dict    *zhdict.Dict
	dir     string
	tmp     string
	reqs    chan segReq
	frames  []frame
	delay   int
	caption string
	cols    int
	rows    int
	px      float64
	fontf   string
	bpx     float64 // the banner's font
	bfontf  string
}

func runDemo(dictDir, actions, out string) error {
	var in io.Reader = os.Stdin
	if actions != "-" {
		fh, err := os.Open(actions)
		if err != nil {
			return err
		}
		defer fh.Close()
		in = fh
	}
	dir, err := zhdict.FindDir(dictDir)
	if err != nil {
		return err
	}
	dict, err := zhdict.Open(dir)
	if err != nil {
		return err
	}
	if err := dict.CheckExtra(); err != nil {
		return err
	}
	// Cards and bookmarks go to a directory thrown away after, so the
	// demo neither adds to cards.tsv nor finds the word carded there.
	tmp, err := os.MkdirTemp("", "zhread-demo")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_STATE_HOME", tmp)
	dm := &demo{dict: dict, dir: dir, tmp: tmp, delay: 500, cols: 90,
		rows: 26, px: 20, bpx: 22}
	sc := bufio.NewScanner(in)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := dm.do(line); err != nil {
			return fmt.Errorf("%s:%d: %v", actions, n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if dm.v == nil {
		return errors.New("no open in the actions")
	}
	dm.v.s.Fini()
	return dm.write(out)
}

func (dm *demo) do(line string) error {
	name, args, _ := strings.Cut(line, " ")
	args = strings.TrimSpace(args)
	ints := func(n int) ([]int, error) {
		f := strings.Fields(args)
		if len(f) < n {
			return nil, fmt.Errorf("%s needs %d numbers", name, n)
		}
		var out []int
		for _, s := range f[:n] {
			i, err := strconv.Atoi(s)
			if err != nil {
				return nil, err
			}
			out = append(out, i)
		}
		return out, nil
	}
	if dm.v == nil && name != "size" && name != "font" &&
		name != "banner" && name != "open" && name != "delay" &&
		name != "caption" {
		return fmt.Errorf("%s before open", name)
	}
	switch name {
	case "size":
		n, err := ints(2)
		if err != nil {
			return err
		}
		dm.cols, dm.rows = n[0], n[1]
	case "font":
		f := strings.Fields(args)
		if len(f) == 0 {
			return errors.New("font needs a size")
		}
		px, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			return err
		}
		dm.px = px
		if len(f) > 1 {
			dm.fontf = strings.Join(f[1:], " ")
		}
	case "banner":
		f := strings.Fields(args)
		if len(f) == 0 {
			return errors.New("banner needs a size")
		}
		px, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			return err
		}
		dm.bpx = px
		if len(f) > 1 {
			dm.bfontf = strings.Join(f[1:], " ")
		}
	case "open":
		return dm.open(args)
	case "find":
		for l, text := range dm.v.d.lines {
			if c := strings.Index(string(text), args); c >= 0 {
				dm.v.cl = l
				s := string(text)
				dm.v.cc = utf8.RuneCountInString(s[:c])
				dm.snap("")
				return nil
			}
		}
		return fmt.Errorf("%q is not in the text", args)
	case "line":
		n, err := ints(1)
		if err != nil {
			return err
		}
		dm.v.cl, dm.v.cc = n[0]-1, 0
		dm.snap("")
	case "delay":
		n, err := ints(1)
		if err != nil {
			return err
		}
		dm.delay = n[0]
	case "caption":
		dm.caption = args
		if args == "none" {
			dm.caption = ""
		}
		if dm.v != nil {
			dm.snap("")
		}
	case "forget":
		os.Remove(dm.v.rec.path)
		rec, err := loadRecord(dm.v.rec.path)
		if err != nil {
			return err
		}
		*dm.v.rec = *rec
	case "pause":
		n, err := ints(1)
		if err != nil {
			return err
		}
		if len(dm.frames) > 0 {
			dm.frames[len(dm.frames)-1].ms += n[0]
		}
	case "keys":
		ks, err := parseKeys(args)
		if err != nil {
			return err
		}
		for _, k := range ks {
			dm.v.key(k.ev)
			dm.snap(k.label)
		}
	case "click", "drag":
		n, err := ints(map[string]int{"click": 2, "drag": 4}[name])
		if err != nil {
			return err
		}
		mouse := func(x, y int, b tcell.ButtonMask) {
			dm.v.mouse(tcell.NewEventMouse(x, y, b, tcell.ModNone))
		}
		mouse(n[0], n[1], tcell.Button1)
		dm.snap("click")
		if name == "drag" {
			x, y := n[0], n[1]
			for x != n[2] || y != n[3] {
				x += sign(n[2] - x)
				if x == n[2] {
					y += sign(n[3] - y)
				}
				mouse(x, y, tcell.Button1)
				dm.snap("drag")
			}
		}
		mouse(n[len(n)-2], n[len(n)-1], tcell.ButtonNone)
	default:
		return fmt.Errorf("unknown action %q", name)
	}
	return nil
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func (dm *demo) open(path string) error {
	d, err := loadDoc(path)
	if err != nil {
		return err
	}
	rec, err := loadRecord(filepath.Join(dm.tmp, "cards.tsv"))
	if err != nil {
		return err
	}
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		return err
	}
	s.SetSize(dm.cols, dm.rows)
	s.SetStyle(stDef)
	dm.s = s
	dm.reqs = make(chan segReq, 1)
	dm.v = &view{s: s, level: &level{d: d}, dict: dm.dict, rec: rec,
		reqs: dm.reqs, convDir: filepath.Join(dm.dir, "opencc"),
		deck: "demo", pron: zhdict.PronCN, showEn: true}
	dm.settle()
	return nil
}

// settle draws the screen and segments what it asks for, as the worker
// would, until it asks for nothing more.
func (dm *demo) settle() {
	for {
		dm.v.draw()
		dm.v.requestSeg()
		select {
		case rq := <-dm.reqs:
			rq.doc.apply(segment(dm.dict, dm.v.pron, rq))
		default:
			return
		}
	}
}

// snap settles the screen and keeps it as a frame, or when it is the
// same as the last, shows the last longer.
func (dm *demo) snap(key string) {
	dm.settle()
	cells, w, h := dm.s.GetContents()
	cx, cy, vis := dm.s.GetCursor()
	f := frame{cells: append([]tcell.SimCell(nil), cells...), w: w,
		h: h, cx: cx, cy: cy, cursor: vis, caption: dm.caption,
		keys: key, ms: dm.delay}
	for i := range f.cells {
		f.cells[i].Bytes = nil
		f.cells[i].Runes = append([]rune(nil), f.cells[i].Runes...)
	}
	dm.frames = append(dm.frames, f)
}

// The font: a cell is half an em wide, a Chinese character two cells.
type demoFont struct {
	face                font.Face
	cw, ch, ascent, pad int // pad is the margin round the screen
	top                 int // the banner's height, above the screen
}

// fontFile finds a font with Chinese in it: fc-match's choice of a
// monospace font for Chinese (a sans-serif one for the banner, with mono
// false), else of any font for it, else the first fc-list has that this
// can read.
func fontFile(mono bool) (string, error) {
	var cands []string
	pats := []string{"monospace:lang=zh-cn", ":lang=zh-cn:spacing=mono"}
	if !mono {
		pats = []string{"sans-serif:lang=zh-cn"}
	}
	for _, pat := range append(pats, ":lang=zh-cn", ":lang=zh-tw") {
		out, err := exec.Command("fc-match", "-f", "%{file}",
			pat).Output()
		if err == nil {
			cands = append(cands, string(out))
		}
	}
	if out, err := exec.Command("fc-list", ":lang=zh",
		"file").Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			cands = append(cands, strings.TrimSuffix(l, ": "))
		}
	}
	for _, f := range cands {
		if f != "" && hasHan(f) {
			return f, nil
		}
	}
	return "", errors.New("found no font with Chinese in it (with " +
		"fc-match and fc-list); give one with the font action")
}

func hasHan(path string) bool {
	fs, err := loadFonts(path)
	if err != nil {
		return false
	}
	for _, f := range fs {
		var b sfnt.Buffer
		if i, err := f.GlyphIndex(&b, '中'); err == nil && i != 0 {
			return true
		}
	}
	return false
}

// loadFonts reads a font file, or each font of a collection.
func loadFonts(path string) ([]*opentype.Font, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	offs := []int{0}
	if len(b) >= 12 && string(b[:4]) == "ttcf" {
		n := int(binary.BigEndian.Uint32(b[8:]))
		offs = nil
		for i := range n {
			if 16+4*i > len(b) {
				break
			}
			offs = append(offs,
				int(binary.BigEndian.Uint32(b[12+4*i:])))
		}
	}
	var out []*opentype.Font
	for _, o := range offs {
		sb, err := standalone(b, o)
		if err != nil {
			return nil, err
		}
		f, err := opentype.Parse(sb)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// standalone copies the font whose table directory starts at off in b to
// a file of its own, each table starting on four bytes as x/image's parser
// insists: WenQuanYi Zen Hei's do not.
func standalone(b []byte, off int) ([]byte, error) {
	be := binary.BigEndian
	if off+12 > len(b) {
		return nil, errors.New("not a font")
	}
	n := int(be.Uint16(b[off+4:]))
	hdr := 12 + 16*n
	if off+hdr > len(b) {
		return nil, errors.New("not a font")
	}
	out := append([]byte(nil), b[off:off+hdr]...)
	for i := range n {
		rec := 12 + 16*i
		o := int(be.Uint32(out[rec+8:]))
		l := int(be.Uint32(out[rec+12:]))
		if o+l > len(b) {
			return nil, errors.New("a table runs past the file")
		}
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		be.PutUint32(out[rec+8:], uint32(len(out)))
		out = append(out, b[o:o+l]...)
	}
	return out, nil
}

func openFont(path string, px float64, mono bool) (*demoFont, error) {
	if path == "" {
		var err error
		if path, err = fontFile(mono); err != nil {
			return nil, err
		}
	}
	fs, err := loadFonts(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	// In a collection, the monospace one, as WenQuanYi Zen Hei Mono, or
	// for the banner the first that is not, nor a bitmap one (Sharp).
	f := fs[0]
	for _, g := range fs {
		var b sfnt.Buffer
		name, _ := g.Name(&b, sfnt.NameIDFamily)
		isMono := strings.Contains(name, "Mono")
		if mono && isMono || !mono && !isMono &&
			!strings.Contains(name, "Sharp") {
			f = g
			break
		}
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px,
		DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	m := face.Metrics()
	return &demoFont{face: face, cw: int(px+1) / 2, pad: int(px / 2),
		ch:     (m.Ascent + m.Descent).Ceil() + 2,
		ascent: m.Ascent.Ceil() + 1}, nil
}

// shades is how many levels of a glyph's coverage are told apart, between
// its background and its foreground.
const shades = 6

// palette maps a cell's colours, and a level of coverage between them, to
// the index of the GIF palette's colour.
type demoPalette struct {
	colors color.Palette
	index  map[[3]int32]uint8 // fg, bg, level
}

func (p *demoPalette) add(fg, bg int32) {
	for l := 0; l <= shades; l++ {
		k := [3]int32{fg, bg, int32(l)}
		if l == 0 {
			k[0] = bg
		}
		if l == shades {
			k[1] = fg
		}
		if _, ok := p.index[k]; ok || len(p.colors) == 256 {
			continue
		}
		mix := func(a, b int32) uint8 {
			return uint8((a*int32(shades-l) + b*int32(l)) / shades)
		}
		c := color.RGBA{mix(bg>>16&255, fg>>16&255),
			mix(bg>>8&255, fg>>8&255), mix(bg&255, fg&255), 255}
		p.index[k] = uint8(len(p.colors))
		p.colors = append(p.colors, c)
	}
}

func (p *demoPalette) at(fg, bg int32, l int) uint8 {
	k := [3]int32{fg, bg, int32(l)}
	if l == 0 {
		k[0] = bg
	}
	if l == shades {
		k[1] = fg
	}
	if i, ok := p.index[k]; ok {
		return i
	}
	return 0 // past 256 colours
}

// colors are a cell's foreground and background as RGB.
func colors(st tcell.Style) (fg, bg int32, bold, ul bool) {
	f, b, attr := st.Decompose()
	fg, bg = 0xd7d7d7, 0x3a3a3a
	if f.Valid() {
		fg = f.Hex()
	}
	if b.Valid() {
		bg = b.Hex()
	}
	if attr&tcell.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	return fg, bg, attr&tcell.AttrBold != 0, st.GetUnderlineStyle() !=
		tcell.UnderlineStyleNone
}

// The banner's colours, a light ground under zhread's dark one, and the
// key just pressed's, so neither looks part of the terminal.
const (
	bannerFg, bannerBg int32 = 0x202020, 0xeee8d5
	keyFg, keyBg       int32 = 0xffffff, 0x4a6fa5
)

// write renders the frames and writes them as a GIF. Each frame after the
// first holds only the cells that changed, the rest showing through.
func (dm *demo) write(out string) error {
	fnt, err := openFont(dm.fontf, dm.px, true)
	if err != nil {
		return err
	}
	bfnt, err := openFont(dm.bfontf, dm.bpx, false)
	if err != nil {
		return err
	}
	fnt.top = bfnt.ch + 2*bfnt.pad
	pal := &demoPalette{index: map[[3]int32]uint8{}}
	pal.colors = color.Palette{color.RGBA{0, 0, 0, 0}} // transparent
	pal.add(bannerFg, bannerBg)
	pal.add(keyFg, keyBg)
	for _, f := range dm.frames {
		for _, c := range f.cells {
			fg, bg, _, _ := colors(c.Style)
			pal.add(fg, bg)
			if bg != 0xffffff {
				pal.add(bg, fg) // for the prompt's cursor
			}
		}
	}
	g := &gif.GIF{}
	var prev *image.Paletted
	for _, f := range dm.frames {
		img := dm.render(f, fnt, bfnt, pal)
		r := img.Bounds()
		if prev != nil {
			r = diff(prev, img)
			if r.Empty() {
				g.Delay[len(g.Delay)-1] += f.ms / 10
				continue
			}
		}
		sub := image.NewPaletted(r, pal.colors)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			a := img.PixOffset(r.Min.X, y)
			b := img.PixOffset(r.Max.X, y)
			copy(sub.Pix[sub.PixOffset(r.Min.X, y):], img.Pix[a:b])
		}
		g.Image = append(g.Image, sub)
		g.Delay = append(g.Delay, f.ms/10)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
		prev = img
	}
	g.Config = image.Config{ColorModel: pal.colors,
		Width: prev.Bounds().Dx(), Height: prev.Bounds().Dy()}
	fh, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(fh, g); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// diff is the smallest rectangle holding every pixel that differs.
func diff(a, b *image.Paletted) image.Rectangle {
	r := image.Rectangle{}
	bd := b.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			i := b.PixOffset(x, y)
			if a.Pix[i] != b.Pix[i] {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}

// render draws a frame: the banner, with the caption at the left and the
// key just pressed at the right, then the screen.
func (dm *demo) render(f frame, fnt, bfnt *demoFont,
	pal *demoPalette) *image.Paletted {
	w := f.w*fnt.cw + 2*fnt.pad
	img := image.NewPaletted(image.Rect(0, 0, w,
		fnt.top+f.h*fnt.ch+2*fnt.pad), pal.colors)
	_, bg, _, _ := colors(stDef)
	bgi := pal.at(bg, bg, 0)
	bani := pal.at(bannerFg, bannerBg, 0)
	for y := 0; y < img.Rect.Dy(); y++ {
		c := bgi
		if y < fnt.top {
			c = bani
		}
		for x := 0; x < w; x++ {
			img.Pix[img.PixOffset(x, y)] = c
		}
	}
	base := bfnt.pad + bfnt.ascent
	right := w - bfnt.pad
	if f.keys != "" {
		kw := textWidth(bfnt, f.keys)
		kpad := bfnt.pad / 2
		x0 := right - kw - 2*kpad
		ki := pal.at(keyFg, keyBg, 0)
		for y := bfnt.pad / 2; y < fnt.top-bfnt.pad/2; y++ {
			for x := x0; x < right; x++ {
				img.Pix[img.PixOffset(x, y)] = ki
			}
		}
		drawText(img, bfnt, pal, x0+kpad, base, right, f.keys, keyFg,
			keyBg)
		right = x0 - bfnt.pad
	}
	drawText(img, bfnt, pal, fnt.pad, base, right, f.caption, bannerFg,
		bannerBg)
	for y := 0; y < f.h; y++ {
		for x := 0; x < f.w; x++ {
			c := f.cells[y*f.w+x]
			r := ' '
			if len(c.Runes) > 0 {
				r = c.Runes[0]
			}
			st := c.Style
			if f.cursor && x == f.cx && y == f.cy {
				st = st.Reverse(true)
			}
			w := max(zhdict.Width(string(r)), 1)
			dm.cell(img, fnt, pal, x, y, w, r, st)
			x += w - 1
		}
	}
	return img
}

// textWidth is how many pixels text takes in fnt's proportional face.
func textWidth(fnt *demoFont, text string) int {
	w := 0
	for _, r := range text {
		if a, ok := fnt.face.GlyphAdvance(r); ok {
			w += a.Round()
		}
	}
	return w
}

// drawText draws text from x along the baseline at base, stopping before
// it would pass right.
func drawText(img *image.Paletted, fnt *demoFont, pal *demoPalette,
	x, base, right int, text string, fg, bg int32) {
	for _, r := range text {
		adv, ok := fnt.face.GlyphAdvance(r)
		if !ok {
			continue
		}
		if x+adv.Round() > right {
			return
		}
		glyph(img, fnt, pal, fixed.P(x, base), r, fg, bg, nil)
		x += adv.Round()
	}
}

// glyph draws r's coverage at dot, as levels between bg and fg. With keep
// it skips a pixel keep says to leave as it is.
func glyph(img *image.Paletted, fnt *demoFont, pal *demoPalette,
	dot fixed.Point26_6, r rune, fg, bg int32, keep func(i, l int) bool) {
	dr, mask, mp, _, ok := fnt.face.Glyph(dot, r)
	if !ok {
		return
	}
	for yy := dr.Min.Y; yy < dr.Max.Y; yy++ {
		for xx := dr.Min.X; xx < dr.Max.X; xx++ {
			if !(image.Point{xx, yy}.In(img.Rect)) {
				continue
			}
			_, _, _, a := mask.At(mp.X+xx-dr.Min.X,
				mp.Y+yy-dr.Min.Y).RGBA()
			l := (int(a)*shades + 0x7fff) / 0xffff
			if l == 0 {
				continue
			}
			i := img.PixOffset(xx, yy)
			if keep != nil && keep(i, l) {
				continue
			}
			img.Pix[i] = pal.at(fg, bg, l)
		}
	}
}

// cell draws rune r, w cells wide, at column x, row y.
func (dm *demo) cell(img *image.Paletted, fnt *demoFont, pal *demoPalette,
	x, y, w int, r rune, st tcell.Style) {
	fg, bg, bold, ul := colors(st)
	px, py := fnt.pad+x*fnt.cw, fnt.top+fnt.pad+y*fnt.ch
	bgi := pal.at(fg, bg, 0)
	for yy := py; yy < py+fnt.ch; yy++ {
		for xx := px; xx < px+w*fnt.cw; xx++ {
			img.Pix[img.PixOffset(xx, yy)] = bgi
		}
	}
	if ul {
		fgi := pal.at(fg, bg, shades)
		yy := py + fnt.ascent + 2
		for xx := px; xx < px+w*fnt.cw; xx++ {
			img.Pix[img.PixOffset(xx, yy)] = fgi
		}
	}
	if r == ' ' || r == 0 {
		return
	}
	// The glyph centred in its cells.
	adv, ok := fnt.face.GlyphAdvance(r)
	if !ok {
		return
	}
	ox := px + (w*fnt.cw-adv.Round())/2
	strokes := []int{0}
	if bold {
		strokes = []int{0, 1}
	}
	for _, sx := range strokes {
		// over a lighter level already drawn, by the bold stroke,
		// keep the darker
		var keep func(i, l int) bool
		if sx > 0 {
			keep = func(i, l int) bool {
				return img.Pix[i] != bgi &&
					shadeOf(pal, fg, bg, img.Pix[i]) >= l
			}
		}
		glyph(img, fnt, pal, fixed.P(ox+sx, py+fnt.ascent), r, fg, bg,
			keep)
	}
}

// shadeOf is the level of index i between bg and fg.
func shadeOf(pal *demoPalette, fg, bg int32, i uint8) int {
	for l := shades; l > 0; l-- {
		if pal.at(fg, bg, l) == i {
			return l
		}
	}
	return 0
}
