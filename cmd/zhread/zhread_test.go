package main

import (
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caleb-lindgren/zhread/zhdict"
	"github.com/gdamore/tcell/v2"
)

func TestTokenize(t *testing.T) {
	line := []rune("我愛 abc12，「好」")
	seg := func(run []rune) []string { return []string{string(run)} }
	got := tokenize(line, seg)
	want := []tok{{0, 2, true}, {3, 8, false}, {8, 10, false},
		{10, 11, true}, {11, 12, false}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %v, want %v", i, got[i], want[i])
		}
	}
	// A segmenter that loses characters falls back to single ones.
	bad := func([]rune) []string { return []string{"我"} }
	if ts := tokenize([]rune("我愛"), bad); len(ts) != 2 {
		t.Errorf("fallback gave %v", ts)
	}
}

// ctxOf returns the context newSpan picks for the first occurrence of word.
func ctxOf(text, word string) string {
	t := []rune(text)
	ws := strings.Index(text, word)
	ws = len([]rune(text[:ws]))
	we := ws + len([]rune(word))
	dr := &draft{p: para{text: t}, ws: ws, we: we,
		span: newSpan(t, ws, we)}
	ctx, _, _ := dr.context()
	return string(ctx)
}

func TestSpan(t *testing.T) {
	cases := []struct{ text, word, want string }{
		// The sentence holding the word.
		{"天氣很好。我們明天去公園散步，然後吃飯。他不想去。", "公園",
			"我們明天去公園散步，然後吃飯。"},
		// Too short alone, so the sentence before is added; the
		// closing quote stays with its sentence.
		{"他問我要不要一起去看電影。「好。」", "好",
			"他問我要不要一起去看電影。「好。」"},
		// Too long, so trimmed to the clauses nearest the word.
		{"這是第一個很長很長的分句，這是第二個很長很長的分句，" +
			"這裡有我們要的詞語，這是第四個很長很長的分句，" +
			"這是第五個很長很長的分句。", "詞語",
			"這是第二個很長很長的分句，這裡有我們要的詞語，" +
				"這是第四個很長很長的分句，這是第五個很長很長的分句。"},
	}
	for _, c := range cases {
		if got := ctxOf(c.text, c.word); got != c.want {
			t.Errorf("%s in %s:\n got %s\nwant %s", c.word, c.text,
				got, c.want)
		}
	}
}

func TestSpanMove(t *testing.T) {
	text := []rune("甲乙丙。丁戊己，庚辛壬癸。")
	s := newSpan(text, 4, 5) // 丁
	s.move('}')              // already at the end
	if s.b != s.n() {
		t.Errorf("b = %d, want %d", s.b, s.n())
	}
	s.move('{') // drop 庚辛壬癸。
	s.move('{') // cannot drop the word's clause
	if got := string(text[s.bounds[s.a]:s.bounds[s.b]]); got !=
		"甲乙丙。丁戊己，" {
		t.Errorf("after { {: %s", got)
	}
}

func TestDups(t *testing.T) {
	rec := &record{words: map[string]bool{}}
	rec.add(carded{simp: "交流", trad: "交流"})
	rec.add(carded{simp: "头发", trad: "頭髮"})
	rec.add(carded{trad: "企業"})
	cases := map[string]int{
		"交流":  1, // the same word
		"交流感": 1, // contains a carded word
		"流":   1, // part of a carded word
		"頭髮":  1, // matched through the traditional form
		"企业":  0, // simplified form unknown for 企業
		"企業家": 1,
		"天氣":  0,
	}
	for w, n := range cases {
		if got := len(rec.dups(w)); got != n {
			t.Errorf("dups(%s) = %d, want %d", w, got, n)
		}
	}
}

func TestMark(t *testing.T) {
	ctx := []rune("好人有好報<b>")
	got := mark(ctx, 0, 1, "")
	if got != "<b>好</b>人有<b>好</b>報&lt;b&gt;" {
		t.Errorf("context: %s", got)
	}
	blank := `<span class=cloze>[…]</span>`
	want := blank + "人有" + blank + "報&lt;b&gt;"
	if got := mark(ctx, 0, 1, gap); got != want {
		t.Errorf("cloze: %s", got)
	}
}

// newTestView opens text in a reader on a simulated screen, with the whole
// file segmented, and cards going to a temporary file.
func newTestView(t *testing.T, text string) (*view,
	tcell.SimulationScreen) {
	t.Helper()
	dir, err := zhdict.FindDir("")
	if err != nil {
		t.Skipf("dictionaries unavailable: %v", err)
	}
	dict, err := zhdict.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The expected screens are those of the full set of dictionaries.
	if m := dict.MissingOptional(); len(m) > 0 {
		t.Skipf("optional dictionaries unavailable (make dict "+
			"ARGS=\"--csld --wikt\"): %s", strings.Join(m, ", "))
	}
	tmp := t.TempDir()
	file := filepath.Join(tmp, "book.txt")
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", tmp)
	d, err := loadDoc(file)
	if err != nil {
		t.Fatal(err)
	}
	rq := segReq{}
	for l, line := range d.lines {
		rq.lines = append(rq.lines, l)
		rq.text = append(rq.text, line)
	}
	d.apply(segment(dict, zhdict.PronCN, rq))
	rec, err := loadRecord(filepath.Join(tmp, "cards.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(60, 20)
	s.SetStyle(stDef)
	v := &view{s: s, level: &level{d: d}, dict: dict, rec: rec,
		reqs:    make(chan segReq, 100),
		convDir: filepath.Join(dir, "opencc"), deck: "test",
		pron: zhdict.PronCN}
	v.draw()
	return v, s
}

// keys sends each rune of ks as a key press; ⏎ is Enter, ⎋ Escape.
func keys(v *view, ks string) {
	for _, r := range ks {
		k := tcell.KeyRune
		switch r {
		case '⏎':
			k = tcell.KeyEnter
		case '⎋':
			k = tcell.KeyEscape
		}
		v.key(tcell.NewEventKey(k, r, tcell.ModNone))
		v.draw()
	}
}

// screen returns the simulated screen's text, one string per row.
func screen(s tcell.SimulationScreen) []string {
	cells, w, h := s.GetContents()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			for _, r := range cells[y*w+x].Runes {
				sb.WriteRune(r)
				// the next cell is this rune's right half
				x += zhdict.Width(string(r)) - 1
			}
		}
		rows[y] = sb.String()
	}
	return rows
}

func onScreen(s tcell.SimulationScreen, text string) bool {
	return strings.Contains(strings.Join(screen(s), "\n"), text)
}

const book = "第一章\n\n我们今天讨论了企业交流的问题，大家都很高兴。" +
	"后来他把头发剪短了，看起来很精神。\n"

func TestNavigation(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "jj")
	if v.cl != 2 || v.cc != 0 {
		t.Fatalf("jj: at %d:%d", v.cl, v.cc)
	}
	// w moves by segmented word: 我们 / 今天 / 讨论 ...
	keys(v, "ww")
	if v.cc != 4 {
		t.Errorf("ww: column %d, want 4 (讨论)", v.cc)
	}
	keys(v, "b")
	if v.cc != 2 {
		t.Errorf("b: column %d, want 2 (今天)", v.cc)
	}
	// w and b pass over punctuation: 问题 / ， / 大家
	keys(v, "0/问题⏎w")
	if string(v.d.lines[2][v.cc:v.cc+2]) != "大家" {
		t.Errorf("w over ，: at column %d", v.cc)
	}
	keys(v, "b")
	if string(v.d.lines[2][v.cc:v.cc+2]) != "问题" {
		t.Errorf("b over ，: at column %d", v.cc)
	}
	keys(v, "$")
	if v.cc != len(v.d.lines[2])-1 {
		t.Errorf("$: column %d", v.cc)
	}
	keys(v, "0gg")
	if v.cl != 0 {
		t.Errorf("gg: line %d", v.cl)
	}
	keys(v, "G")
	if v.cl != len(v.d.lines)-1 {
		t.Errorf("G: line %d", v.cl)
	}
	keys(v, "gg/头发⏎")
	if v.cl != 2 || string(v.d.lines[2][v.cc:v.cc+2]) != "头发" {
		t.Errorf("/头发: at %d:%d", v.cl, v.cc)
	}
	keys(v, "gg/问题⏎")
	keys(v, "*")
	if string(v.d.lines[v.cl][v.cc:v.cc+2]) != "问题" {
		t.Errorf("*: at %d:%d", v.cl, v.cc)
	}
	// gj moves a display row; the paragraph wraps at 60 columns.
	keys(v, "0gj")
	if v.cl != 2 || v.cc == 0 {
		t.Errorf("gj: at %d:%d", v.cl, v.cc)
	}
}

// TestAnchored checks that the cursor's row stays the same number of rows
// from the top whatever the definitions pane below needs, except near the
// start, and at the end too.
func TestAnchored(t *testing.T) {
	v, _ := newTestView(t, strings.Repeat("好。\n须臾。\n", 100))
	row := func() int { return v.rowOf(v.cl, v.cc) - v.top }
	if keys(v, "j"); v.top != 0 || row() != 1 {
		t.Errorf("near the start: top %d, row %d", v.top, row())
	}
	heights := map[int]bool{}
	for _, k := range []string{"100j", "j", "k", "40k", "G"} {
		keys(v, k)
		heights[v.textH()] = true
		if row() != v.anchor() {
			t.Errorf("after %s: row %d, want %d",
				k, row(), v.anchor())
		}
	}
	if len(heights) < 2 {
		t.Errorf("the pane kept one height: %v", heights)
	}
	if v.cl != 199 {
		t.Errorf("G: at line %d", v.cl)
	}
}

func TestPane(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/企业⏎")
	if !onScreen(s, "企業（～业）  qǐ yè") || onScreen(s, "TW qì yè") {
		t.Errorf("not the Mainland reading alone for 企业:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "t")
	if !onScreen(s, "qǐ yè (TW qì yè)") {
		t.Errorf("no Taiwan reading after t:\n%s",
			strings.Join(screen(s), "\n"))
	}
	if onScreen(s, "enterprise") {
		t.Errorf("English shown before e")
	}
	keys(v, "e")
	if !onScreen(s, "enterprise") {
		t.Errorf("English not shown after e")
	}
	keys(v, "gh")
	if !onScreen(s, "character left, line down") ||
		onScreen(s, "enterprise") {
		t.Errorf("gh does not show the keys:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋")
	if !onScreen(s, "enterprise") {
		t.Errorf("Esc does not close the keys")
	}
}

func TestSelText(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "/讨论⏎vl:")
	if v.mode != prompt || !v.selecting() {
		t.Fatalf(": in visual mode: mode %d, selecting %v", v.mode,
			v.selecting())
	}
	if got := v.selText(); got != "讨论" {
		t.Errorf("selText %q, want 讨论", got)
	}
	keys(v, "⎋")
	if v.mode != normal {
		t.Errorf("mode %d after Esc at the : prompt", v.mode)
	}
}

func TestBookmark(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "jjm")
	if l := v.d.getMark(); l != 2 {
		t.Fatalf("mark at %d, want 2", l)
	}
	keys(v, "ggjm")
	if l := v.d.getMark(); l != 1 {
		t.Errorf("second mark at %d, want 1", l)
	}
	keys(v, "G'")
	if v.cl != 1 {
		t.Errorf("' went to %d", v.cl)
	}
}

func TestMakeCard(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/头发⏎vl⏎")
	if v.mode != card {
		t.Fatalf("not in the card preview, mode %d", v.mode)
	}
	if !onScreen(s, "后来他把头发剪短了，看起来很精神。") ||
		!onScreen(s, "後來他把頭髮剪短了，看起來很精神。") {
		t.Errorf("preview lacks the two contexts:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⏎")
	b, err := os.ReadFile(v.rec.path)
	if err != nil {
		t.Fatal(err)
	}
	// TrimSuffix, not TrimSpace: the empty last columns end the row in tabs
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if lines[0] != "#separator:tab" || len(lines) != 7 {
		t.Fatalf("card file:\n%s", b)
	}
	f := strings.Split(lines[6], "\t")
	if len(f) != 1+len(fields) {
		t.Fatalf("%d fields: %q", len(f), f)
	}
	want := map[int]string{
		1: "头发", 2: "頭髮",
		6:  "后来他把<b>头发</b>剪短了，看起来很精神。",
		7:  "后来他把<span class=cloze>[简体]</span>剪短了，看起来很精神。",
		8:  "後來他把<b>頭髮</b>剪短了，看起來很精神。",
		9:  "後來他把<span class=cloze>[繁體]</span>剪短了，看起來很精神。",
		10: v.d.src + ":3",
	}
	if !filepath.IsAbs(f[10]) {
		t.Errorf("source %q is not a full path", f[10])
	}
	for i, w := range want {
		if f[i] != w {
			t.Errorf("field %s = %q, want %q",
				fields[i-1], f[i], w)
		}
	}
	if !strings.HasPrefix(f[3], "tóu fa") {
		t.Errorf("pinyin %q", f[3])
	}

	// The same word again is a duplicate: the first Enter warns, offering
	// to add to the card instead, the second saves.
	keys(v, "/头发⏎vl⏎⏎")
	if v.mode != card || !onScreen(s, "u: edit") {
		t.Fatalf("no duplicate warning:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋")
	if n := len(v.rec.cards); n != 1 {
		t.Errorf("%d cards after canceling, want 1", n)
	}
}

// TestUpdateCard checks that u opens the word's existing card in the
// preview, as the card has it, and that Enter saves it back over its row.
func TestUpdateCard(t *testing.T) {
	v, s := newTestView(t, "他把头发剪短了。\n\n她的头发很长，很好看。\n")
	keys(v, "/头发⏎vl⏎")
	if len(v.dr.others) != 0 {
		t.Errorf("a new card has %d other contexts", len(v.dr.others))
	}
	keys(v, "⏎")
	if len(v.rec.cards) != 1 {
		t.Fatalf("%d cards", len(v.rec.cards))
	}
	guid := v.rec.cards[0].guid
	row0, _ := v.rec.row(guid)
	// A definition no dictionary has stays on the card as it is.
	b, _ := os.ReadFile(v.rec.path)
	b = []byte(strings.Replace(string(b), row0[5], row0[5]+"<br>my note",
		1))
	os.WriteFile(v.rec.path, b, 0o644)
	if err := v.rec.update(guid, func([]string) {}); err != nil {
		t.Fatal(err)
	}
	keys(v, "gg/头发⏎n⏎u")
	dr := v.dr
	if v.mode != card || dr.guid != guid ||
		!onScreen(s, "edit card: 頭髮") {
		t.Fatalf("not editing:\n%s", strings.Join(screen(s), "\n"))
	}
	if string(dr.p.text) != "他把头发剪短了。" {
		t.Errorf("own context %q", string(dr.p.text))
	}
	if len(dr.others) != 1 || !dr.osel[0] ||
		string(dr.others[0].ctxf.ctxS) != "她的头发很长，很好看。" {
		t.Errorf("others: %d %v", len(dr.others), dr.osel)
	}
	// the card's definitions are ticked, its own one among them
	raw := -1
	for i, o := range dr.opts {
		if o.raw == "my note" {
			raw = i
		}
	}
	if raw < 0 || !dr.sel[raw] {
		t.Fatalf("my note: %d", raw)
	}
	// untick the first definition, tick the second, and save
	keys(v, " j ⏎")
	if v.mode != normal || len(v.rec.cards) != 1 ||
		!strings.HasPrefix(v.msg, "updated") {
		t.Fatalf("mode %d, %d cards, %q", v.mode, len(v.rec.cards),
			v.msg)
	}
	row, err := v.rec.row(guid)
	if err != nil {
		t.Fatal(err)
	}
	if row[11] != row0[11] || row[6] != row0[6] {
		t.Errorf("added %q, context %q", row[11], row[6])
	}
	if !strings.Contains(row[12], "她的<b>头发</b>很长") {
		t.Errorf("contexts %q", row[12])
	}
	if !strings.Contains(row[5], "my note") || row[4] == row0[4] {
		t.Errorf("definitions %q %q, were %q", row[4], row[5], row0[4])
	}
}

// click presses the left button at x, y and lets it go.
func click(v *view, x, y int) {
	v.mouse(tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
	v.mouse(tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
}

// special sends a key other than a character.
func special(v *view, k tcell.Key, mod tcell.ModMask) {
	v.key(tcell.NewEventKey(k, 0, mod))
	v.draw()
}

func TestArrows(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "jj")
	// arrows move by word, and with Ctrl by character
	special(v, tcell.KeyRight, tcell.ModNone)
	if v.cc != 2 {
		t.Errorf("→: column %d, want 2 (今天)", v.cc)
	}
	special(v, tcell.KeyRight, tcell.ModCtrl)
	if v.cc != 3 {
		t.Errorf("^→: column %d", v.cc)
	}
	special(v, tcell.KeyLeft, tcell.ModCtrl)
	special(v, tcell.KeyLeft, tcell.ModNone)
	if v.cc != 0 {
		t.Errorf("^← ←: column %d, want 0 (我们)", v.cc)
	}
	// ^← at a line's start goes to the end of the line above
	special(v, tcell.KeyLeft, tcell.ModCtrl)
	special(v, tcell.KeyLeft, tcell.ModCtrl)
	if v.cl != 0 || v.cc != 2 {
		t.Errorf("^←^←: at %d:%d, want 0:2", v.cl, v.cc)
	}
	special(v, tcell.KeyDown, tcell.ModCtrl)
	if v.cl != 2 || v.cc != 0 {
		t.Errorf("^↓: at %d:%d, want 2:0", v.cl, v.cc)
	}
	special(v, tcell.KeyUp, tcell.ModCtrl)
	if v.cl != 0 {
		t.Errorf("^↑: line %d", v.cl)
	}
	special(v, tcell.KeyEnd, tcell.ModCtrl)
	if v.cl != 2 || v.cc != len(v.d.lines[2])-1 {
		t.Errorf("^End: at %d:%d", v.cl, v.cc)
	}
	special(v, tcell.KeyHome, tcell.ModNone)
	row := v.rows[v.rowOf(v.cl, v.cc)]
	if v.cc != row.start || row.start == 0 {
		t.Errorf("Home: column %d, row from %d", v.cc, row.start)
	}
	special(v, tcell.KeyEnd, tcell.ModNone)
	if v.cc != row.end-1 {
		t.Errorf("End: column %d", v.cc)
	}
	special(v, tcell.KeyHome, tcell.ModCtrl)
	if v.cl != 0 || v.cc != 0 {
		t.Errorf("^Home: at %d:%d", v.cl, v.cc)
	}

	// Shift selects; a move without it ends the selection.
	keys(v, "jj")
	special(v, tcell.KeyRight, tcell.ModShift)
	special(v, tcell.KeyRight, tcell.ModShift|tcell.ModCtrl)
	if v.mode != visual || v.selText() != "我们今天" {
		t.Errorf("Shift: mode %d, selected %q", v.mode, v.selText())
	}
	special(v, tcell.KeyRight, tcell.ModNone)
	if v.mode != normal {
		t.Errorf("→ after Shift: mode %d", v.mode)
	}
	// but not one begun with v
	keys(v, "v")
	special(v, tcell.KeyRight, tcell.ModNone)
	if v.mode != visual {
		t.Errorf("→ after v: mode %d", v.mode)
	}
}

func TestDrag(t *testing.T) {
	v, _ := newTestView(t, book)
	m := func(x int, b tcell.ButtonMask) {
		v.mouse(tcell.NewEventMouse(x, 2, b, tcell.ModNone))
		v.draw()
	}
	m(0, tcell.Button1)
	if v.mode != normal {
		t.Fatalf("a press selects")
	}
	m(4, tcell.Button1)
	m(7, tcell.Button1)
	m(7, tcell.ButtonNone)
	if v.mode != visual || v.selText() != "我们今天" {
		t.Errorf("drag: mode %d, selected %q", v.mode, v.selText())
	}
	click(v, 0, 2)
	if v.mode != normal {
		t.Errorf("a click leaves the selection")
	}
}

func TestSplitCard(t *testing.T) {
	v, s := newTestView(t, book)
	// 讨论了企业 is no dictionary's word, so it becomes a card for
	// each word in it that has a definition: 讨论, 了, 企业.
	keys(v, "/讨论了企业⏎vllll⏎")
	if v.mode != card || v.dr.of != 3 {
		t.Fatalf("mode %d, %d cards:\n%s", v.mode, v.dr.of,
			strings.Join(screen(s), "\n"))
	}
	if !onScreen(s, "new card: 討論（讨论）") || !onScreen(s, "(1 of 3)") {
		t.Errorf("first card:\n%s", strings.Join(screen(s), "\n"))
	}
	keys(v, "⏎n") // save 讨论, skip 了
	if !onScreen(s, "new card: 企業（～业）") {
		t.Errorf("third card:\n%s", strings.Join(screen(s), "\n"))
	}
	keys(v, "⏎")
	if v.mode != normal {
		t.Errorf("mode %d after the last card", v.mode)
	}
	var got []string
	for _, c := range v.rec.cards {
		got = append(got, c.simp)
	}
	if strings.Join(got, " ") != "讨论 企业" {
		t.Errorf("saved %v, want 讨论 企业", got)
	}
}

func TestPaneFits(t *testing.T) {
	v, s := newTestView(t, book)
	// want is the pane's height for what it shows: the rule and the
	// rows its lines wrap to, at most half the screen.
	want := func() int {
		n := 1
		for _, l := range v.pane() {
			n += v.height(l.x, l.wx, l.text)
		}
		return min(n, v.h/2)
	}
	// On the blank line there is no word: the pane is just its rule.
	keys(v, "j")
	if v.ph != 1 {
		t.Errorf("blank line: pane %d rows, want 1", v.ph)
	}
	keys(v, "j")
	wide := v.ph
	if wide < 2 || wide != want() {
		t.Errorf("我们: pane %d rows, want %d", v.ph, want())
	}
	if v.textH() != v.h-1-wide {
		t.Errorf("text %d rows with a %d-row pane", v.textH(), wide)
	}
	// After the terminal narrows, the pane is measured again.
	s.SetSize(30, 20)
	v.rows = nil
	v.draw()
	if v.ph <= wide || v.ph != want() {
		t.Errorf("at 30 columns: pane %d rows, want %d (%d at 60)",
			v.ph, want(), wide)
	}
}

func TestBackSearch(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "G$?企业⏎")
	if v.cl != 2 || string(v.d.lines[2][v.cc:v.cc+2]) != "企业" {
		t.Errorf("?企业: at %d:%d", v.cl, v.cc)
	}
}

// paneText is the text of every line the pane would show, scrolled or
// not.
func paneText(v *view) string {
	var sb strings.Builder
	for _, l := range v.pane() {
		sb.WriteString(string(l.text) + "\n")
	}
	return sb.String()
}

// TestQuotations checks the dictionaries the pane shows: 兩岸詞典 for
// Chinese, MoEDict as well on c; and that x shows quotations, which are
// hidden at first, while examples are always on the definition's line.
func TestQuotations(t *testing.T) {
	v, s := newTestView(t, "他好高兴。须臾\n")
	s.SetSize(70, 40)
	v.rows = nil
	keys(v, "l")
	if !onScreen(s, "1.美善的；理想的（與「壞」相對）。 如：「美好」") {
		t.Errorf("no 兩岸詞典 sense with its examples:\n%s",
			strings.Join(screen(s), "\n"))
	}
	if strings.Contains(paneText(v), "MoE") {
		t.Errorf("MoEDict shown before c")
	}
	keys(v, "c")
	if p := paneText(v); !strings.Contains(p, "MoE") ||
		!strings.Contains(p, "① 美、善，理想的。 如：「好東西」") {
		t.Errorf("no MoEDict after c:\n%s", p)
	}
	// The entry is now longer than the pane: J scrolls it.
	if !onScreen(s, "↓") {
		t.Fatalf("no note of rows below:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "J")
	if v.pscroll != 1 || !onScreen(s, "↑1") {
		t.Errorf("J: scrolled %d", v.pscroll)
	}
	keys(v, "$h")
	if v.pscroll != 0 {
		t.Errorf("scroll kept for the next word: %d", v.pscroll)
	}
	if !onScreen(s, "〈書〉遷延") || onScreen(s, "淮陰") {
		t.Errorf("须臾 before x:\n%s", strings.Join(screen(s), "\n"))
	}
	keys(v, "x")
	if !onScreen(s, "淮陰") {
		t.Errorf("no quotation after x:\n%s",
			strings.Join(screen(s), "\n"))
	}
}

// TestSenseLayout checks that the senses under a part of speech line up
// after it, and sub-senses under their heading.
func TestSenseLayout(t *testing.T) {
	v, s := newTestView(t, "我的书\n")
	s.SetSize(120, 60)
	v.rows = nil
	keys(v, "lc")
	col := func(text string) int {
		for _, l := range v.pane() {
			lt := string(l.text)
			if i := strings.Index(lt, text); i >= 0 {
				return l.x + zhdict.Width(lt[:i])
			}
		}
		t.Fatalf("no %q:\n%s", text, paneText(v))
		return 0
	}
	if a, b := col("1.用在作定語"), col("2.用在名詞"); a != b {
		t.Errorf("兩岸詞典 senses at %d and %d", a, b)
	}
	head, sub := col("① 結構助詞："), col("(1) 置於形容詞後。")
	if col("(3) 置於修飾") != sub || col("(4) 置於副詞後") != sub ||
		sub != head+2 || col("② 句尾助詞") != head {
		t.Errorf("MoEDict senses misaligned:\n%s", paneText(v))
	}
}

// TestCardSenses checks that a definition is offered a sense at a time,
// MoEDict's only after c, and that a card carries quotations only when x
// showed them.
func TestCardSenses(t *testing.T) {
	v, s := newTestView(t, "他好高兴。须臾\n")
	keys(v, "lvl⏎")
	if v.mode != card {
		t.Fatalf("mode %d", v.mode)
	}
	count := func(prefix string) int {
		n := 0
		for _, o := range v.dr.opts[:v.dr.zh] {
			if strings.HasPrefix(o.s.Text, prefix) {
				n++
			}
		}
		return n
	}
	if count("1.美善的") != 1 || count("① 美、善") != 0 || v.dr.zh < 10 {
		t.Fatalf("%d options for 好", v.dr.zh)
	}
	// The list scrolls to keep the cursor's option in view.
	s.SetSize(60, 20)
	v.rows = nil
	keys(v, strings.Repeat("j", v.dr.zh-1))
	if txt := v.dr.opts[v.dr.cur].s.Text; !onScreen(s, txt) {
		t.Errorf("option %d (%s) not in view:\n%s", v.dr.cur, txt,
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋c/好⏎vl⏎")
	if count("1.美善的") != 1 || count("① 美、善") != 1 {
		t.Errorf("MoEDict's senses not offered after c")
	}

	keys(v, "⎋$hv$⏎")
	f := v.dr.forms(v.conv)
	cur := -1
	for i, o := range v.dr.opts {
		if len(o.s.Cites) > 0 {
			cur = i
			break
		}
	}
	if cur < 0 {
		t.Fatalf("no quotation among 须臾's options")
	}
	v.dr.sel[cur] = true
	cite := v.dr.opts[cur].s.Cites[0]
	if note := v.dr.note(f, false, false); strings.Contains(note[4],
		cite) {
		t.Errorf("quotation on a card made without x")
	}
	if note := v.dr.note(f, false, true); !strings.Contains(note[4],
		cite) {
		t.Errorf("no quotation on a card made with x")
	}
}

// TestWiktionary checks that Wiktionary defines a word CC-CEDICT lacks,
// and is shown beside CC-CEDICT, labeled, only after E.
func TestWiktionary(t *testing.T) {
	v, _ := newTestView(t, "最大\n土豆\n")
	keys(v, "vle")
	if p := paneText(v); !strings.Contains(p, "largest") ||
		strings.Contains(p, "Wiktionary") {
		t.Errorf("最大, which CC-CEDICT lacks:\n%s", p)
	}
	keys(v, "⎋j")
	if p := paneText(v); !strings.Contains(p, "potato; (Tw) peanut") ||
		strings.Contains(p, "Wiktionary") {
		t.Errorf("土豆 before E:\n%s", p)
	}
	keys(v, "E")
	// the source goes under the word, here in the gap left of the
	// definition's second line
	if p := paneText(v); !strings.Contains(p, "\nWiktionary  ") ||
		strings.Contains(p, "tǔ dòu  Wiktionary") {
		t.Errorf("土豆 after E:\n%s", p)
	}
}

func TestCardedSense(t *testing.T) {
	v, _ := newTestView(t, "土豆\n")
	g := v.d.probe[0].Rows("土豆")
	sn := g.Senses(g.Zh)[0] // CC-CEDICT's
	v.rec.add(carded{simp: "土豆", en: opt{s: sn}.html(false) +
		"<br>a quotation"})
	keys(v, "e")
	p := paneText(v)
	if !strings.Contains(p, sn.Text+" (carded)\n") {
		t.Errorf("carded sense not marked:\n%s", p)
	}
	if strings.Count(p, "(carded)") != 1 {
		t.Errorf("want one (carded):\n%s", p)
	}
	// a card with none of the senses shown is noted under the word
	v.rec.add(carded{simp: "最大"})
	v2, _ := newTestView(t, "最大\n")
	v2.rec = v.rec
	keys(v2, "vle")
	if p := paneText(v2); !strings.Contains(p, "\n(carded)") {
		t.Errorf("最大:\n%s", p)
	}
}

func TestClick(t *testing.T) {
	v, _ := newTestView(t, book)
	// Row 2 is line 2; each Han character is two columns wide, so
	// column 7 is the right half of 天, the fourth character.
	click(v, 7, 2)
	if v.cl != 2 || v.cc != 3 {
		t.Fatalf("click: at %d:%d, want 2:3", v.cl, v.cc)
	}
	// a click below the text does nothing
	v.draw()
	click(v, 0, v.textH())
	if v.cl != 2 || v.cc != 3 {
		t.Errorf("click on the pane: at %d:%d", v.cl, v.cc)
	}
}

func TestVisualNoWordHighlight(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "jjw") // on 今天
	cells, w, _ := s.GetContents()
	// 天, after the cursor on 今, is in the word's colors
	if st := cells[2*w+6].Style; st != stCurWord {
		t.Fatalf("normal: rest of the word not highlighted")
	}
	keys(v, "v")
	cells, w, _ = s.GetContents()
	if st := cells[2*w+6].Style; st != stDef {
		t.Errorf("visual: rest of the word still highlighted")
	}
}

// segAll segments the lines of the level being read, as the worker would.
func segAll(v *view) {
	rq := segReq{doc: v.d, chars: v.chars}
	for l, line := range v.d.lines {
		rq.lines = append(rq.lines, l)
		rq.text = append(rq.text, line)
	}
	v.d.apply(segment(v.dict, zhdict.PronCN, rq))
	v.draw()
}

// words lists the words of line l, as segmented.
func words(v *view, l int) []string {
	var ws []string
	for _, t := range v.d.toks[l] {
		if t.han {
			ws = append(ws, string(v.d.lines[l][t.start:t.end]))
		}
	}
	return ws
}

// TestChars checks that s switches between words and single characters,
// and that a batch segmented before the switch is not applied after it.
func TestChars(t *testing.T) {
	v, _ := newTestView(t, "道可道，非常道。\n")
	want := []string{"道", "可", "道", "非常", "道"}
	if got := words(v, 0); !slices.Equal(got, want) {
		t.Fatalf("words: got %v, want %v", got, want)
	}
	stale := segReq{doc: v.d, lines: []int{0}, text: v.d.lines[:1]}
	res := segment(v.dict, zhdict.PronCN, stale)
	keys(v, "s")
	if !v.chars || v.d.toks[0] != nil {
		t.Fatalf("s did not switch to characters and forget the words")
	}
	v.d.apply(res) // segmented as words, before s
	if v.d.toks[0] != nil {
		t.Fatalf("a batch segmented before s was applied after it")
	}
	segAll(v)
	want = []string{"道", "可", "道", "非", "常", "道"}
	if got := words(v, 0); !slices.Equal(got, want) {
		t.Errorf("characters: got %v, want %v", got, want)
	}
	keys(v, "s")
	segAll(v)
	if got := words(v, 0); len(got) != 5 {
		t.Errorf("s again: got %v, want words", got)
	}
}

func tab(v *view, back bool) {
	k := tcell.KeyTab
	if back {
		k = tcell.KeyBacktab
	}
	v.key(tcell.NewEventKey(k, 0, tcell.ModNone))
	v.draw()
}

func TestNestedPanes(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "jjww") // on 讨论
	pane := paneText(v)
	tab(v, false)
	if len(v.stack) != 1 || paneText(v) != "…\n" {
		t.Fatalf("Tab: depth %d, pane before segmenting:\n%s",
			len(v.stack), paneText(v))
	}
	// the text is now the pane that was, the cursor on its first row,
	// past the word and its reading to the definition
	var text strings.Builder
	for _, l := range v.d.lines {
		text.WriteString(string(l) + "\n")
	}
	if text.String() != pane || v.cl != 0 || v.d.lines[0][v.cc] != '交' {
		t.Fatalf("Tab: at %d:%d of\n%s", v.cl, v.cc, text.String())
	}
	// 0, h and b do not go back onto the word or reading
	for _, k := range []string{"0", "h", "b", "k"} {
		keys(v, k)
		if v.cl != 0 || v.d.lines[0][v.cc] != '交' {
			t.Errorf("%s: at %d:%d", k, v.cl, v.cc)
		}
	}
	segAll(v)
	w, _ := v.lookup()
	if w != "交換" || !strings.HasPrefix(paneText(v), w) {
		t.Errorf("in the pane: %q, pane:\n%s", w, paneText(v))
	}
	// The pane below keeps its height, so the text does not move as the
	// cursor goes from word to word.
	ph, y := v.ph, v.blocks[0].y
	keys(v, "w")
	if w2, _ := v.lookup(); w2 == w || v.ph != ph || v.blocks[0].y != y {
		t.Errorf("w to %q: pane %d rows, text at %d; was %d, %d",
			w2, v.ph, v.blocks[0].y, ph, y)
	}
	keys(v, "b")
	t.Logf("depth 1:\n%s", strings.Join(screen(s), "\n"))
	tab(v, false)
	segAll(v)
	if len(v.stack) != 2 {
		t.Fatalf("second Tab: depth %d", len(v.stack))
	}
	t.Logf("depth 2:\n%s", strings.Join(screen(s), "\n"))
	if !strings.HasSuffix(v.d.name, "› 讨论 › "+w) {
		t.Errorf("name %q", v.d.name)
	}
	tab(v, true)
	tab(v, true)
	if len(v.stack) != 0 || v.cl != 2 || v.cc != 4 {
		t.Errorf("back up: depth %d at %d:%d", len(v.stack), v.cl,
			v.cc)
	}
	// a click in the pane moves into it, one in the book back up
	py := v.h - v.ph
	click(v, 0, py)
	v.draw()
	if len(v.stack) != 1 || v.cl != 0 {
		t.Fatalf("click in the pane: depth %d at %d", len(v.stack),
			v.cl)
	}
	b := v.blocks[1] // the book's
	click(v, 0, b.y+b.n-1)
	if len(v.stack) != 0 {
		t.Errorf("click in the book: depth %d", len(v.stack))
	}
}

// TestOneScript checks that the preview shows a word the same in both
// scripts, which gets only the simplified cards, in the text's script only.
func TestOneScript(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/精神⏎vl⏎")
	if !onScreen(s, "简  后来他把头发剪短了") || onScreen(s, "繁  ") {
		t.Errorf("simplified text:\n%s", strings.Join(screen(s), "\n"))
	}
	v, s = newTestView(t, "後來他把頭髮剪短了，看起來很精神。\n")
	keys(v, "/精神⏎vl⏎")
	if !onScreen(s, "繁  後來他把頭髮剪短了") || onScreen(s, "简  ") {
		t.Errorf("traditional text:\n%s", strings.Join(screen(s), "\n"))
	}
}

// cardFields saves a card for the first match of word in text and returns
// its fields.
func cardFields(t *testing.T, text, word string) []string {
	t.Helper()
	v, _ := newTestView(t, text)
	keys(v, "/"+word+"⏎v"+strings.Repeat("l",
		utf8.RuneCountInString(word)-1)+"⏎⏎")
	b, err := os.ReadFile(v.rec.path)
	if err != nil {
		t.Fatal(err)
	}
	// TrimSuffix, not TrimSpace: the empty last columns end the row in tabs
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	return strings.Split(lines[len(lines)-1], "\t")
}

// TestCardScript checks that a word the same in both scripts gets only
// the cards in the script of the text it came from.
func TestCardScript(t *testing.T) {
	f := cardFields(t, "後來他把頭髮剪短了，看起來很精神。\n", "精神")
	if f[2] != "精神" || f[6] != "" || f[7] != "" ||
		f[8] != "後來他把頭髮剪短了，看起來很<b>精神</b>。" {
		t.Errorf("traditional text: %q", f)
	}
	f = cardFields(t, book, "精神")
	if f[2] != "" || f[8] != "" ||
		f[6] != "后来他把头发剪短了，看起来很<b>精神</b>。" {
		t.Errorf("simplified text: %q", f)
	}
}

// TestEnglishOnly checks that a word with no Chinese definition shows its
// English at once, in the pane and ticked on the card, without e.
func TestEnglishOnly(t *testing.T) {
	v, _ := newTestView(t, "最大\n")
	keys(v, "vl")
	if p := paneText(v); !strings.Contains(p, "largest") {
		t.Errorf("pane:\n%s", p)
	}
	keys(v, "⏎")
	if v.dr == nil || v.dr.zh != 0 || len(v.dr.sel) == 0 ||
		!v.dr.sel[0] {
		t.Errorf("card: %+v", v.dr)
	}
}

// TestOtherContexts checks that a new card carries only the context it
// was made from, and that each card made for the same word after it that
// is added to it with u adds its context, keeping those before, so the
// card's contexts grow by one each time.
func TestOtherContexts(t *testing.T) {
	v, _ := newTestView(t, "他散发传单给路人。\n\n"+
		"她身上散发香味，又散发热气。\n\n花儿散发着芬芳的香气。\n")
	keys(v, "lvl⏎")
	if v.mode != card {
		t.Fatalf("mode %d", v.mode)
	}
	dr := v.dr
	note := dr.note(dr.forms(v.conv), false, false)
	if len(dr.others) != 0 || note[12] != "" || note[13] != "" {
		t.Fatalf("a new card has other contexts: %d %q %q",
			len(dr.others), note[12], note[13])
	}
	keys(v, "⏎")
	if len(v.rec.cards) != 1 {
		t.Fatalf("%d cards", len(v.rec.cards))
	}
	guid := v.rec.cards[0].guid
	// add makes a card for the word from the n-th place the search finds
	// it, which warns that it is carded; u adds to the card instead, and
	// Enter saves it over its row.
	add := func(n int) []string {
		t.Helper()
		keys(v, "gg/散发⏎"+strings.Repeat("n", n)+"⏎⏎")
		if v.mode != card || !v.dr.saving {
			t.Fatalf("no duplicate warning at place %d", n)
		}
		keys(v, "u")
		if v.dr.guid != guid {
			t.Fatalf("u did not open the card")
		}
		keys(v, "⏎")
		if v.mode != normal || len(v.rec.cards) != 1 ||
			!strings.HasPrefix(v.msg, "updated") {
			t.Fatalf("mode %d, %d cards, %q", v.mode,
				len(v.rec.cards), v.msg)
		}
		row, err := v.rec.row(guid)
		if err != nil {
			t.Fatal(err)
		}
		if row[6] != "他<b>散发</b>传单给路人。" {
			t.Errorf("own context %q", row[6])
		}
		return row
	}
	p2 := "<div>她身上<b>散发</b>香味，又<b>散发</b>热气。</div>"
	p3 := "<div>花儿<b>散发</b>着芬芳的香气。</div>"
	if row := add(1); row[12] != p2 {
		t.Errorf("second: Contexts %q", row[12])
	}
	if row := add(3); row[12] != p2+p3 {
		t.Errorf("third: Contexts %q", row[12])
	}
	// a place whose context the card has adds nothing
	if row := add(2); row[12] != p2+p3 {
		t.Errorf("again: Contexts %q", row[12])
	}

	// The edges and the editor work on the context under the cursor,
	// and the other script follows an edit of the text's own.
	keys(v, "gg/散发⏎⏎⏎u")
	dr = v.dr
	if len(dr.others) != 2 || !dr.osel[0] || !dr.osel[1] {
		t.Fatalf("others %d %v", len(dr.others), dr.osel)
	}
	dr.cur = len(dr.opts)
	keys(v, "{")
	if got := string(dr.others[0].ctxf.ctxS); got != "她身上散发香味" {
		t.Errorf("after {: %q", got)
	}
	keys(v, "ix⎋")
	if got := string(dr.others[0].ctxf.ctxS); got != "身上散发香味" {
		t.Errorf("after Delete: %q", got)
	}
	if got := string(dr.others[0].ctxf.ctxT); got != "身上散發香味" {
		t.Errorf("traditional after Delete: %q", got)
	}
	if dr.editing || dr.others[0].vi.insert {
		t.Errorf("still editing after Esc")
	}
	// space unticks one, which leaves the card
	keys(v, "j ")
	if dr.osel[1] {
		t.Errorf("space left %v", dr.osel)
	}
	note = dr.note(dr.forms(v.conv), false, false)
	if note[12] != "<div>身上<b>散发</b>香味</div>" {
		t.Errorf("Contexts after the edits %q", note[12])
	}
	if !strings.Contains(string(note[6]), "他<b>散发</b>传单给路人。") {
		t.Errorf("own context changed: %q", note[6])
	}
}

// TestDoubled checks that a word inside a doubled form of it is widened
// to the whole form, and only then.
func TestDoubled(t *testing.T) {
	for _, c := range []struct{ text, word, want string }{
		{"她松松垮垮地坐着", "松垮", "松松垮垮"},
		{"我们研究研究吧", "研究", "研究研究"},
		{"你看看这个", "看", "看看"},
		{"他看了看我", "看", "看了看"},
		{"你看一看", "看", "看一看"},
		{"我们去采访他", "采访", "采访"},
		{"他看我", "看", "看"},
	} {
		r := []rune(c.text)
		ws := len([]rune(c.text[:strings.Index(c.text, c.word)]))
		a, b := doubled(r, ws, ws+utf8.RuneCountInString(c.word))
		if got := string(r[a:b]); got != c.want {
			t.Errorf("%s in %s: %s, want %s", c.word, c.text, got,
				c.want)
		}
	}
}

// TestOwnDoubled checks that a card's own context marks and blanks a
// doubled form of the word whole, keeping the word itself as the card's.
func TestOwnDoubled(t *testing.T) {
	v, _ := newTestView(t, "我看着电梯工，她松松垮垮地坐着。\n")
	keys(v, "/松垮⏎vl⏎")
	if v.mode != card {
		t.Fatalf("mode %d", v.mode)
	}
	note := v.dr.note(v.dr.forms(v.conv), false, false)
	if note[1] != "松垮" || note[2] != "鬆垮" {
		t.Errorf("word %q %q", note[1], note[2])
	}
	blank := "她<span class=cloze>[简体]</span>地"
	if !strings.Contains(note[6], "她<b>松松垮垮</b>地") ||
		!strings.Contains(note[7], blank) ||
		!strings.Contains(note[8], "她<b>鬆鬆垮垮</b>地") {
		t.Errorf("contexts %q", note[6:10])
	}
}

// TestDiscard checks that an Esc right after another asks before it
// throws away edits to a context, and that only y then does.
func TestDiscard(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/精神⏎⏎ki0x⎋⎋")
	if v.mode != card || !v.dr.discard ||
		!onScreen(s, "throw away the changes") {
		t.Fatalf("no question after ⎋⎋: mode %d", v.mode)
	}
	keys(v, "⎋")
	if v.mode != card || v.dr.discard {
		t.Fatalf("Esc to the question did not keep the card")
	}
	keys(v, "⎋y")
	if v.mode != normal {
		t.Errorf("y did not throw the card away")
	}
	// One Esc on its own cancels at once, edits or none.
	keys(v, "/精神⏎⏎ki0x⎋j⎋")
	if v.mode != normal {
		t.Errorf("a lone Esc did not cancel")
	}
	// So do two with nothing changed.
	keys(v, "/精神⏎⏎ki⎋⎋")
	if v.mode != normal {
		t.Errorf("Esc Esc without changes did not cancel")
	}
	// An edge moved or a tick changed is a change too, asked about
	// when the Esc out of the editor is followed by another.
	for _, k := range []string{"k[", " k"} {
		keys(v, "/精神⏎⏎"+k+"i⎋⎋")
		if v.mode != card || !v.dr.discard {
			t.Errorf("%q then Esc Esc did not ask", k)
		}
		keys(v, "y")
	}
}

// TestCardMode checks the mode marker at the bottom of the card preview.
func TestCardMode(t *testing.T) {
	v, s := newTestView(t, "他散发传单给路人。\n\n"+
		"她身上散发香味，又散发热气。\n")
	// the card made from the first, edited from the second with u, has
	// the second's context below its definitions, for jjjj to reach
	keys(v, "lvl⏎⏎gg/散发⏎n⏎u")
	mode := func() string {
		rows := screen(s)
		return strings.Fields(rows[len(rows)-1])[1]
	}
	for _, c := range []struct{ keys, want string }{
		{"", "LIST"}, {"kk", "LIST"}, {"i", "NORMAL"},
		{"i", "INSERT"}, {"⎋", "NORMAL"}, {"⎋", "LIST"},
		{"ij", "LIST"}, {"jjjj", "LIST"}, {"i", "NORMAL"},
		{"i", "INSERT"}, {"⎋", "NORMAL"}, {"k", "LIST"},
		{"ji⎋", "LIST"},
	} {
		keys(v, c.keys)
		if got := mode(); got != c.want {
			t.Errorf("after %q: %s, want %s", c.keys, got, c.want)
		}
	}
}

// cur is the character under the cursor.
func cur(v *view) string {
	line := v.d.lines[v.cl]
	if v.cc >= len(line) {
		return ""
	}
	return string(line[v.cc])
}

// TestPaneColumns checks that j and k keep the cursor's column on screen
// across a pane's lines, which start at different columns, and pass over
// the sense numbers.
func TestPaneColumns(t *testing.T) {
	v, s := newTestView(t, "牟\n")
	tab(v, false)
	segAll(v)
	if cur(v) != "謀" {
		t.Fatalf("Tab: on %q:\n%s", cur(v),
			strings.Join(screen(s), "\n"))
	}
	for _, c := range []struct{ keys, want string }{
		{"j", "擬"}, {"h", "擬"}, {"k", "謀"}, {"0", "謀"},
		{"j0", "擬"},
	} {
		keys(v, c.keys)
		if cur(v) != c.want {
			t.Errorf("%s: on %q, want %q", c.keys, cur(v), c.want)
		}
	}
	// Esc goes back to the book from however deep.
	tab(v, false)
	segAll(v)
	keys(v, "⎋")
	if len(v.stack) != 0 || v.cl != 0 || v.cc != 0 {
		t.Errorf("Esc: depth %d at %d:%d", len(v.stack), v.cl, v.cc)
	}
}

// TestNumbering checks that the cursor passes over a paragraph's number.
func TestNumbering(t *testing.T) {
	v, _ := newTestView(t, "1. 我們\n2、他們\n")
	if cur(v) != "我" {
		t.Errorf("start: on %q", cur(v))
	}
	keys(v, "j0h")
	if cur(v) != "他" {
		t.Errorf("j0h: on %q", cur(v))
	}
}

// TestEnterInNormal checks that Enter makes a card of the word under the
// cursor without a selection.
func TestEnterInNormal(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/头发⏎⏎")
	if v.mode != card || !onScreen(s, "new card: 頭髮（头发）") {
		t.Errorf("mode %d:\n%s", v.mode, strings.Join(screen(s), "\n"))
	}
}

// TestChoose checks that a word the dictionaries divide differently asks
// which words to card.
func TestChoose(t *testing.T) {
	v, s := newTestView(t, "他們牟取暴利。\n")
	keys(v, "/牟⏎⏎")
	if v.mode != choose || len(v.pick.opts) != 2 ||
		!onScreen(s, "牟取 · 暴利") {
		t.Fatalf("mode %d:\n%s", v.mode, strings.Join(screen(s), "\n"))
	}
	keys(v, "j⏎")
	if v.mode != card || v.dr.of != 2 || !onScreen(s, "new card: 牟取") {
		t.Errorf("mode %d:\n%s", v.mode, strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋/牟⏎⏎1")
	if v.mode != card || v.dr.of != 1 ||
		!onScreen(s, "new card: 牟取暴利") {
		t.Errorf("1: mode %d:\n%s", v.mode,
			strings.Join(screen(s), "\n"))
	}
}

// TestPaneContext checks that a card made in a pane takes the sense it is
// in as its context, without the word, reading and number before it.
func TestPaneContext(t *testing.T) {
	v, s := newTestView(t, "牟\n")
	tab(v, false)
	segAll(v)
	keys(v, "⏎")
	if v.mode != card {
		t.Fatalf("mode %d:\n%s", v.mode, strings.Join(screen(s), "\n"))
	}
	ctx, _, _ := v.dr.context()
	if got := string(ctx); !strings.HasPrefix(got, "謀取。") {
		t.Errorf("context %q", got)
	}
}

// TestContextEnd checks that a context cut short of its sentence's end
// does not end with the comma it was cut at.
func TestContextEnd(t *testing.T) {
	text := "這裡有我們要的詞語，這是第二個很長很長的分句，" +
		"這是第三個很長很長的分句，這是第四個很長很長的分句，" +
		"這是第五個很長很長的分句。"
	got := ctxOf(text, "詞語")
	if strings.HasSuffix(got, "，") || !strings.HasPrefix(got, "這裡") {
		t.Errorf("context %q", got)
	}
}

// TestCardOptions checks that c in the preview adds MoEDict's definitions,
// keeping what was ticked.
// TestCardMouse checks that a click on a card's line moves the cursor to
// it, and one on a tick box ticks or unticks it, once however long the
// button is held.
func TestCardMouse(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "/企业⏎⏎")
	dr := v.dr
	if n := len(v.cardHits); n < 3 {
		t.Fatalf("%d lines to click on, want 3 or more", n)
	}
	var ctx, opt cardHit
	for _, h := range v.cardHits {
		if h.cur < 0 {
			ctx = h
		} else if h.cur == 1 {
			opt = h
		}
	}
	was := dr.sel[1]
	click(v, opt.bx+10, opt.y0) // the text, not the box
	v.draw()
	if dr.cur != 1 || dr.sel[1] != was {
		t.Errorf("click on option 1's text: cur %d, ticked %v; "+
			"want 1, %v", dr.cur, dr.sel[1], was)
	}
	click(v, opt.bx+1, opt.by) // the box
	v.draw()
	if dr.sel[1] == was {
		t.Errorf("click on option 1's box did not tick it")
	}
	// Held and dragged, the button ticks the box once.
	v.mouse(tcell.NewEventMouse(opt.bx, opt.by, tcell.Button1,
		tcell.ModNone))
	v.mouse(tcell.NewEventMouse(opt.bx+1, opt.by, tcell.Button1,
		tcell.ModNone))
	v.mouse(tcell.NewEventMouse(opt.bx+1, opt.by, tcell.ButtonNone,
		tcell.ModNone))
	if dr.sel[1] != was {
		t.Errorf("a held button ticked the box more than once")
	}
	click(v, 5, ctx.y0)
	v.draw()
	if dr.cur != ctx.cur {
		t.Errorf("click on the context: cur %d, want %d",
			dr.cur, ctx.cur)
	}
}

func TestCardOptions(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "/企业⏎⏎")
	n, ticked := len(v.dr.opts), v.dr.opts[v.dr.zh]
	keys(v, "c")
	if len(v.dr.opts) <= n {
		t.Fatalf("c: %d options, had %d", len(v.dr.opts), n)
	}
	var sel []opt
	for i, o := range v.dr.opts {
		if v.dr.sel[i] {
			sel = append(sel, o)
		}
	}
	if len(sel) != 2 || sel[1].s.Text != ticked.s.Text {
		t.Errorf("ticked after c: %+v", sel)
	}
	keys(v, "c")
	if len(v.dr.opts) != n {
		t.Errorf("c again: %d options, want %d", len(v.dr.opts), n)
	}
}

// TestEditContext checks the vi keys on the preview's context.
func TestEditContext(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/精神⏎⏎ki0dwx")
	f := v.dr.forms(v.conv)
	if got := string(f.ctxS); got != "看起来很精神。" {
		t.Fatalf("after 0dwx: %q\n%s", got,
			strings.Join(screen(s), "\n"))
	}
	keys(v, "i他⎋")
	keys(v, "$hhi真⎋")
	f = v.dr.forms(v.conv)
	if got := string(f.ctxS); got != "他看起来很真精神。" ||
		string(f.ctxS[f.wsS:f.weS]) != "精神" {
		t.Fatalf("after inserts: %q, word %q", got,
			string(f.ctxS[f.wsS:f.weS]))
	}
	keys(v, "uu")
	f = v.dr.forms(v.conv)
	if got := string(f.ctxS); got != "看起来很精神。" {
		t.Errorf("after uu: %q", got)
	}
	keys(v, "j⏎")
	got := v.rec.cards[len(v.rec.cards)-1].ctx
	if got != "看起来很精神。" {
		t.Errorf("saved context %q", got)
	}
}

// TestCardKeys checks that the card lists its keys on gh, not in its
// status line, and that Esc hides them before it cancels the card.
func TestCardKeys(t *testing.T) {
	v, s := newTestView(t, "他散发传单给路人。\n")
	keys(v, "lvl⏎")
	status := func() string {
		rows := screen(s)
		return rows[len(rows)-1]
	}
	// no keys, but at the right the toggles that change the card
	if st := strings.TrimSpace(status()); !strings.HasSuffix(st,
		" c E") || strings.Contains(st, "Enter") ||
		strings.Contains(st, "gh") {
		t.Errorf("status line: %q", st)
	}
	keys(v, "gh")
	if !onScreen(s, "edit the context under the cursor") {
		t.Fatalf("gh does not list the keys:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "J")
	if v.cscroll != 1 {
		t.Errorf("J scrolled the keys to %d", v.cscroll)
	}
	keys(v, "⎋")
	if v.mode != card || onScreen(s, "edit the context under") {
		t.Errorf("Esc: mode %d, keys shown %v", v.mode,
			onScreen(s, "edit the context under"))
	}
	// In insert mode g is text.
	keys(v, "kiIg⎋")
	if v.chelp || !onScreen(s, "g他散發") {
		t.Errorf("g in insert mode:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋⎋y")
	if v.mode != normal {
		t.Errorf("Esc did not leave the card: mode %d", v.mode)
	}
}

// TestPaneMarks checks that the pane's rule notes which of x, c and E
// would change what it shows.
func TestPaneMarks(t *testing.T) {
	v, s := newTestView(t, "好\n须臾\n土豆\n")
	s.SetSize(70, 40)
	v.rows = nil
	rule := func() string {
		for _, r := range screen(s) {
			if strings.HasPrefix(r, "──") {
				return r
			}
		}
		return ""
	}
	for _, c := range []struct{ keys, want string }{
		{"", "─ c e  ↓"}, {"j", "─ x c e ─"}, {"j", "─ c e ─"},
		{"e", "─ c e E ─"},
	} {
		keys(v, c.keys)
		if r := rule(); !strings.Contains(r, c.want) {
			t.Errorf("after %q: rule %q, want %q", c.keys, r,
				c.want)
		}
	}
}

// TestCardMarks checks that the card's status line notes x only while a
// ticked definition has quotations.
func TestCardMarks(t *testing.T) {
	v, s := newTestView(t, "须臾\n")
	keys(v, "⏎")
	// the marks, after the buttons
	end := func() string {
		rows := screen(s)
		_, m, _ := strings.Cut(rows[len(rows)-1], "cancel")
		return strings.Join(strings.Fields(m), " ")
	}
	if got := end(); got != "c E" {
		t.Errorf("須臾: marks %q, want %q", got, "c E")
	}
	// its second sense, unticked, has the quotation
	keys(v, "j ")
	if got := end(); got != "x c E" {
		t.Errorf("須臾 with its second sense ticked: marks %q", got)
	}
}

func TestParseKeys(t *testing.T) {
	ks, err := parseKeys("w<C-S-Right>*3 <Space><S-Tab>")
	if err != nil {
		t.Fatal(err)
	}
	if len(ks) != 6 {
		t.Fatalf("%d keys", len(ks))
	}
	k := ks[1].ev
	if k.Key() != tcell.KeyRight ||
		k.Modifiers() != tcell.ModCtrl|tcell.ModShift ||
		ks[1].label != "Ctrl+Shift+→" {
		t.Errorf("<C-S-Right>: %v %v %q", k.Key(), k.Modifiers(),
			ks[1].label)
	}
	if ks[4].ev.Rune() != ' ' || ks[5].ev.Key() != tcell.KeyBacktab {
		t.Errorf("<Space><S-Tab>: %v %v", ks[4].ev.Rune(),
			ks[5].ev.Key())
	}
	if _, err := parseKeys("<Nope>"); err == nil {
		t.Errorf("<Nope> parsed")
	}
}

// TestDemo plays a short demo and checks the GIF has a frame for each
// key that changed the screen.
func TestDemo(t *testing.T) {
	if _, err := zhdict.FindDir(""); err != nil {
		t.Skipf("dictionaries unavailable: %v", err)
	}
	if _, err := fontFile(true); err != nil {
		t.Skipf("no font: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp) // restored after runDemo's
	text := filepath.Join(tmp, "book.txt")
	os.WriteFile(text, []byte(book), 0o644)
	acts := filepath.Join(tmp, "acts")
	os.WriteFile(acts, []byte("size 40 12\nopen "+text+
		"\nfind 我们\ncaption hello\nkeys <C-Right>*2 <S-Right>\n"),
		0o644)
	out := filepath.Join(tmp, "out.gif")
	if err := runDemo("", acts, out, ""); err != nil {
		t.Fatal(err)
	}
	fh, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	g, err := gif.DecodeAll(fh)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 5 {
		t.Errorf("%d frames, want 5", len(g.Image))
	}
}

// TestDemoCards checks that -cards with -demo neither overwrites a file
// nor writes one when the demo makes no card.
func TestDemoCards(t *testing.T) {
	if _, err := zhdict.FindDir(""); err != nil {
		t.Skipf("dictionaries unavailable: %v", err)
	}
	if _, err := fontFile(true); err != nil {
		t.Skipf("no font: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	text := filepath.Join(tmp, "book.txt")
	os.WriteFile(text, []byte(book), 0o644)
	acts := filepath.Join(tmp, "acts")
	os.WriteFile(acts, []byte("size 40 12\nopen "+text+
		"\ncaption hello\n"), 0o644)
	out := filepath.Join(tmp, "out.gif")

	kept := filepath.Join(tmp, "kept.tsv")
	os.WriteFile(kept, []byte("mine\n"), 0o644)
	if err := runDemo("", acts, out, kept); err == nil {
		t.Errorf("an existing -cards file was accepted")
	}
	if b, _ := os.ReadFile(kept); string(b) != "mine\n" {
		t.Errorf("an existing -cards file was changed to %q", b)
	}

	none := filepath.Join(tmp, "none.tsv")
	if err := runDemo("", acts, out, none); err == nil {
		t.Errorf("no error for a demo that made no cards")
	}
	if _, err := os.Stat(none); err == nil {
		t.Errorf("a card file was written for a demo with no cards")
	}
}

// TestDemoMouse checks the demo's mouse actions: a double click on a word
// found by its text makes a card of it, Save clicked saves it, and text not
// on the screen is an error.
func TestDemoMouse(t *testing.T) {
	if _, err := zhdict.FindDir(""); err != nil {
		t.Skipf("dictionaries unavailable: %v", err)
	}
	if _, err := fontFile(true); err != nil {
		t.Skipf("no font: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	text := filepath.Join(tmp, "book.txt")
	os.WriteFile(text, []byte(book), 0o644)
	acts := filepath.Join(tmp, "acts")
	out := filepath.Join(tmp, "out.gif")
	os.WriteFile(acts, []byte("size 60 16\nopen "+text+
		"\ndclick 企业\nclick #-1 save\nwheel 2\n"), 0o644)
	cards := filepath.Join(tmp, "cards.tsv")
	if err := runDemo("", acts, out, cards); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(cards); !strings.Contains(string(b),
		"\t企业\t企業\t") {
		t.Errorf("no card of 企业 saved:\n%s", b)
	}
	os.WriteFile(acts, []byte("open "+text+"\nclick 不在\n"), 0o644)
	if err := runDemo("", acts, out, ""); err == nil ||
		!strings.Contains(err.Error(), "not on the screen") {
		t.Errorf("clicking text not on the screen: %v", err)
	}
}

// TestManyContexts checks that the preview stays quick with thousands of
// other contexts on a card, the cursor at the last of them.
func TestManyContexts(t *testing.T) {
	v, s := newTestView(t, "他散发传单给路人。\n")
	keys(v, "/散发⏎vl⏎")
	dr := v.dr
	o := ctxDraft("她身上<b>散发</b>香味。", "散发", false)
	o.refresh(v.conv)
	for range 5001 {
		dr.others = append(dr.others, o)
		dr.osel = append(dr.osel, true)
	}
	start := time.Now()
	for range 20 {
		v.key(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
		v.draw()
	}
	keys(v, strings.Repeat("j", 20))
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("40 keys took %v", d)
	}
	if dr.cur != 220 || !onScreen(s, "↑") {
		t.Errorf("cursor at %d:\n%s", dr.cur,
			strings.Join(screen(s), "\n"))
	}
}

func TestMoveOther(t *testing.T) {
	a, b := &draft{}, &draft{}
	dr := &draft{opts: make([]opt, 2), others: []*draft{a, b},
		osel: []bool{true, false}, cur: 2}
	dr.moveOther(true)
	if dr.others[0] != b || dr.osel[0] || dr.cur != 3 {
		t.Errorf("down: %v %v %d", dr.others[0] == b, dr.osel, dr.cur)
	}
	dr.moveOther(true) // the last stays
	dr.moveOther(false)
	if dr.others[0] != a || dr.cur != 2 {
		t.Errorf("up: %v %d", dr.others[0] == a, dr.cur)
	}
}

// TestAltWords checks Option-arrows move as Ctrl-arrows do, by character,
// and Esc b, Esc f by word, as macOS terminals send them.
func TestAltWords(t *testing.T) {
	v, _ := newTestView(t, book)
	keys(v, "jj")
	special(v, tcell.KeyRight, tcell.ModAlt)
	special(v, tcell.KeyRight, tcell.ModAlt)
	if v.cc != 2 {
		t.Errorf("Alt-→ twice: column %d", v.cc)
	}
	v.key(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModAlt))
	if v.cc != 4 {
		t.Errorf("Esc f: column %d", v.cc)
	}
	v.key(tcell.NewEventKey(tcell.KeyRune, 'B', tcell.ModAlt))
	if v.cc != 2 || v.mode != visual {
		t.Errorf("Esc B: column %d, mode %d", v.cc, v.mode)
	}
	special(v, tcell.KeyUp, tcell.ModAlt)
	if v.cl != 2 || v.cc != 0 || v.mode != normal {
		t.Errorf("Alt-↑: at %d:%d, mode %d", v.cl, v.cc, v.mode)
	}
}

// TestUndoCard takes back an update, then the card it updated, asking
// first each time.
func TestUndoCard(t *testing.T) {
	v, _ := newTestView(t, "他散发传单给路人。\n\n她身上散发香味。\n")
	keys(v, "u")
	if v.undoAsk || !strings.HasPrefix(v.msg, "no card") {
		t.Fatalf("u with nothing made: %q", v.msg)
	}
	keys(v, "lvl⏎⏎")
	if len(v.rec.cards) != 1 {
		t.Fatalf("%d cards", len(v.rec.cards))
	}
	guid := v.rec.cards[0].guid
	before, _ := v.rec.row(guid)
	keys(v, "gg/散发⏎n⏎⏎u⏎")
	if after, _ := v.rec.row(guid); after[12] == "" {
		t.Fatalf("the update added no context")
	}
	keys(v, "uq")
	if v.msg != "kept it" || len(v.rec.cards) != 1 {
		t.Fatalf("q did not keep it: %q", v.msg)
	}
	keys(v, "uy")
	row, err := v.rec.row(guid)
	if err != nil || !slices.Equal(row, before) {
		t.Fatalf("not put back: %q %v", row, err)
	}
	keys(v, "u")
	if !strings.HasPrefix(v.msg, "delete the card for 散發") {
		t.Fatalf("asks %q", v.msg)
	}
	keys(v, "y")
	if len(v.rec.cards) != 0 || !strings.HasPrefix(v.msg, "deleted") {
		t.Fatalf("%d cards, %q", len(v.rec.cards), v.msg)
	}
	b, _ := os.ReadFile(v.rec.path)
	if strings.Contains(string(b), guid) {
		t.Errorf("the row is still in the file")
	}
	keys(v, "u")
	if v.undoAsk {
		t.Errorf("u asks with nothing left to undo")
	}
}

// find is where text is on the screen, its column and row, or -1, -1.
func find(s tcell.SimulationScreen, text string) (int, int) {
	for y, row := range screen(s) {
		if i := strings.Index(row, text); i >= 0 {
			return zhdict.Width(row[:i]), y
		}
	}
	return -1, -1
}

// clickOn clicks on text where the screen shows it.
func clickOn(t *testing.T, v *view, s tcell.SimulationScreen, text string) {
	t.Helper()
	x, y := find(s, text)
	if y < 0 {
		t.Fatalf("%q is not on the screen:\n%s", text,
			strings.Join(screen(s), "\n"))
	}
	click(v, x, y)
	v.draw()
}

// TestMenu checks the undo button after a card is saved, and the menu
// button's menu, by mouse and by keys.
func TestMenu(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/企业⏎⏎⏎")
	if !strings.HasPrefix(v.msg, "saved a card") {
		t.Fatalf("no card saved: %q", v.msg)
	}
	clickOn(t, v, s, "undo")
	if !v.undoAsk {
		t.Fatalf("a click on undo did not ask: %q", v.msg)
	}
	clickOn(t, v, s, "y: undo it")
	if v.rec.lastUndo() != "" {
		t.Errorf("a click on y: undo it did not undo the card: %q",
			v.msg)
	}
	if x, _ := find(s, "undo"); x >= 0 {
		t.Errorf("undo still shown after undoing:\n%s",
			strings.Join(screen(s), "\n"))
	}
	// A click clears the message, and with it the undo button.
	keys(v, "/企业⏎⏎⏎")
	click(v, 0, 2)
	v.draw()
	if x, _ := find(s, "undo"); x >= 0 {
		t.Errorf("undo still shown after a click")
	}

	clickOn(t, v, s, "menu")
	if !v.menu {
		t.Fatal("a click on menu did not open the menu")
	}
	clickOn(t, v, s, "bookmark this line")
	if v.menu || !strings.HasPrefix(v.msg, "bookmarked") {
		t.Errorf("the menu's bookmark: menu %v, %q", v.menu, v.msg)
	}

	// Off the menu, a click only closes it.
	l, c := v.cl, v.cc
	clickOn(t, v, s, "menu")
	click(v, 2, 2)
	v.draw()
	if v.menu || v.cl != l || v.cc != c {
		t.Errorf("a click off the menu: menu %v, cursor %d,%d; "+
			"want closed, %d,%d", v.menu, v.cl, v.cc, l, c)
	}

	// Keys: ↓ twice and Enter go to the bookmark; an item's own key
	// works with the menu open.
	keys(v, "G")
	clickOn(t, v, s, "menu")
	for _, k := range []tcell.Key{tcell.KeyDown, tcell.KeyDown,
		tcell.KeyEnter} {
		v.key(tcell.NewEventKey(k, 0, tcell.ModNone))
		v.draw()
	}
	if v.menu || v.cl != l {
		t.Errorf("↓ ↓ Enter in the menu: menu %v, line %d; want "+
			"closed, %d", v.menu, v.cl, l)
	}
	clickOn(t, v, s, "menu")
	keys(v, "q")
	if !v.quit {
		t.Errorf("q with the menu open did not quit")
	}
}

// TestControls checks -controls: each way of working left out does
// nothing, and without the mouse no buttons are shown or green.
func TestControls(t *testing.T) {
	if _, err := parseControls("vi,mouse"); err != nil {
		t.Errorf("vi,mouse: %v", err)
	}
	if _, err := parseControls("vi,keys"); err == nil {
		t.Errorf("no error for keys")
	}
	key := func(v *view, k tcell.Key) {
		v.key(tcell.NewEventKey(k, 0, tcell.ModNone))
		v.draw()
	}

	v, s := newTestView(t, book)
	v.noVi = true
	keys(v, "/企业⏎")
	c := v.cc
	keys(v, "w")
	if v.cc != c {
		t.Errorf("w moved without vi's keys")
	}
	key(v, tcell.KeyRight)
	if v.cc == c {
		t.Errorf("→ did not move without vi's keys")
	}
	// The editor is a plain one: i types at once, and Esc leaves it.
	keys(v, "/精神⏎⏎")
	key(v, tcell.KeyUp)
	keys(v, "i他")
	key(v, tcell.KeyEscape)
	if f := v.dr.forms(v.conv); v.dr.editing ||
		!strings.HasPrefix(string(f.ctxS), "他") {
		t.Errorf("editing without vi: editing %v, context %q",
			v.dr.editing, f.ctxS)
	}

	v, s = newTestView(t, book)
	v.noArrows = true
	keys(v, "/企业⏎")
	c = v.cc
	key(v, tcell.KeyRight)
	if v.cc != c {
		t.Errorf("→ moved without the arrows")
	}
	keys(v, "w")
	if v.cc == c {
		t.Errorf("w did not move without the arrows")
	}
	x, y := find(s, "詞")
	if _, _, st, _ := s.GetContent(x, y); st != stSel {
		t.Errorf("詞 is not green as a button")
	}

	// The keys gh lists are those of the ways left.
	help := func(v *view, h []helpKey) string {
		var b strings.Builder
		for _, l := range v.keyLines(h) {
			b.WriteString(string(l.text) + "\n")
		}
		return b.String()
	}
	if h := help(v, keyHelp); strings.Contains(h, "PgUp") ||
		strings.Contains(h, "F1") || !strings.Contains(h, "gh") ||
		!strings.Contains(h, "J K ") {
		t.Errorf("gh without the arrows lists:\n%s", h)
	}
	v.noArrows, v.noVi = false, true
	if h := help(v, cardKeyHelp); strings.Contains(h, "[ ] { }") ||
		strings.Contains(h, "dd cc") || !strings.Contains(h, "S-←") ||
		!strings.Contains(h, "type themselves") {
		t.Errorf("the card's keys without vi's list:\n%s", h)
	}

	v, s = newTestView(t, book)
	v.noMouse = true
	if h := help(v, keyHelp); strings.Contains(h, "double-click") {
		t.Errorf("gh without the mouse lists:\n%s", h)
	}
	keys(v, "/企业⏎⏎⏎")
	if x, _ := find(s, "menu"); x >= 0 {
		t.Errorf("a menu button without the mouse")
	}
	if x, _ := find(s, "undo"); x >= 0 {
		t.Errorf("an undo button without the mouse")
	}
	x, y = find(s, "詞")
	if _, _, st, _ := s.GetContent(x, y); st == stSel {
		t.Errorf("詞 is green without the mouse")
	}
	if len(v.hits) > 0 {
		t.Errorf("%d clickable places without the mouse", len(v.hits))
	}
	keys(v, "⏎")
	if x, _ := find(s, " save "); x >= 0 {
		t.Errorf("card buttons without the mouse")
	}
}

// TestCardMenu checks the card maker's menu, a double click on a context,
// which edits it, and { } in the reader, which move by paragraph.
func TestCardMenu(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "}")
	if v.cl != 2 {
		t.Errorf("} went to line %d, want 2", v.cl)
	}
	keys(v, "{")
	if v.cl != 0 {
		t.Errorf("{ went to line %d, want 0", v.cl)
	}

	s.SetSize(90, 24)
	keys(v, "/精神⏎⏎k")
	clickOn(t, v, s, "menu")
	if !v.menu || !onScreen(s, "edit this context") {
		t.Fatalf("no menu in the card maker:\n%s",
			strings.Join(screen(s), "\n"))
	}
	clickOn(t, v, s, "show keyboard shortcuts")
	if v.menu || !v.chelp {
		t.Errorf("the menu's keys: menu %v, chelp %v", v.menu, v.chelp)
	}
	keys(v, "⎋")

	x, y := find(s, "看起来很精神")
	if y < 0 {
		t.Fatalf("no context on the screen:\n%s",
			strings.Join(screen(s), "\n"))
	}
	click(v, x, y)
	v.draw()
	if v.dr.editing {
		t.Fatalf("one click started editing")
	}
	click(v, x, y)
	v.draw()
	if !v.dr.editing {
		t.Errorf("a double click on the context did not edit it")
	}
}

// TestEditorTypes checks that in the editor's normal mode a key that is
// no vi command types itself, and Backspace and Delete delete.
func TestEditorTypes(t *testing.T) {
	e := &edited{text: []rune("天下")}
	var s vi
	ev := func(r rune) *tcell.EventKey {
		return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
	}
	s.key(ev('人'), e, func() {})
	if string(e.text) != "人天下" || !s.insert || s.col != 1 {
		t.Fatalf("人 in normal mode: %q, insert %v, col %d",
			e.text, s.insert, s.col)
	}
	s.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), e,
		func() {})
	s.col = 1
	s.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), e,
		func() {})
	if string(e.text) != "天下" {
		t.Errorf("Backspace in normal mode: %q", e.text)
	}
	s.key(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone), e,
		func() {})
	if string(e.text) != "下" {
		t.Errorf("Delete in normal mode: %q", e.text)
	}
	if s.key(ev('j'), e, func() {}) {
		t.Errorf("j was taken as text, not left to the list")
	}
}

// TestReaderMouse checks the reader's mouse and the keys for those who
// do not use vi's: a double click makes a card, the wheel over the
// definitions scrolls them, Alt-PgDn too, and a click on a key the rule
// or the status line names is that key.
func TestReaderMouse(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/企业⏎")
	v.mouse(tcell.NewEventMouse(0, v.h-3, tcell.WheelDown, tcell.ModNone))
	if v.pscroll != 1 {
		t.Errorf("wheel over the definitions: pscroll %d, want 1",
			v.pscroll)
	}
	cl := v.cl
	v.pscroll = 0
	// not drawn after, which would clamp the scroll to the entry
	v.key(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModAlt))
	if v.pscroll != 1 || v.cl != cl {
		t.Errorf("Alt-PgDn: pscroll %d, line %d; want 1, %d",
			v.pscroll, v.cl, cl)
	}
	en := v.showEn
	x, y := find(s, " e ")
	if y < 0 {
		t.Fatalf("no e on the rule:\n%s",
			strings.Join(screen(s), "\n"))
	}
	click(v, x+1, y)
	v.draw()
	if v.showEn == en {
		t.Errorf("a click on the rule's e did not toggle English")
	}
	clickOn(t, v, s, "詞")
	if !v.chars {
		t.Errorf("a click on 詞 did not switch to single characters")
	}
	clickOn(t, v, s, "字")
	segAll(v)
	x, y = find(s, "讨论")
	click(v, x, y)
	click(v, x, y)
	v.draw()
	if v.mode != card {
		t.Fatalf("a double click did not make a card")
	}
	if !onScreen(s, "讨论") || !onScreen(s, "new card") {
		t.Errorf("the card is not for 讨论:\n%s",
			strings.Join(screen(s), "\n"))
	}
}

// TestCardButtons checks the card's buttons and the arrow keys for its
// context's edges.
func TestCardButtons(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/企业⏎⏎")
	ctx := func() string {
		r, _, _ := v.dr.context()
		return string(r)
	}
	keys(v, "]")
	want := ctx()
	keys(v, "⎋⏎")
	special(v, tcell.KeyRight, tcell.ModShift)
	if got := ctx(); got != want {
		t.Errorf("Shift-→: context %q, ] gives %q", got, want)
	}
	keys(v, "⎋⏎")
	clickOn(t, v, s, "start>")
	if got := ctx(); got != want {
		t.Errorf("start>: context %q, ] gives %q", got, want)
	}
	clickOn(t, v, s, " cancel ")
	if v.mode != card || !v.dr.discard {
		t.Fatalf("Cancel on an edited card did not ask first")
	}
	clickOn(t, v, s, "y: ")
	if v.mode != normal {
		t.Fatalf("y: yes did not throw the card away")
	}
	keys(v, "⏎")
	clickOn(t, v, s, " save ")
	if v.mode != normal || !strings.HasPrefix(v.msg, "saved a card") {
		t.Errorf("Save: mode %d, message %q", v.mode, v.msg)
	}
}

// TestChooseMouse checks that a click on a way of dividing a word chooses
// it, as Enter on it does.
func TestChooseMouse(t *testing.T) {
	v, s := newTestView(t, "他牟取暴利。\n")
	keys(v, "/牟取⏎⏎")
	if v.mode != choose {
		t.Fatalf("no choice of divisions:\n%s",
			strings.Join(screen(s), "\n"))
	}
	clickOn(t, v, s, "牟取 · 暴利")
	if v.mode != card || !onScreen(s, "(1 of 2)") {
		t.Errorf("click on 牟取 · 暴利: mode %d\n%s", v.mode,
			strings.Join(screen(s), "\n"))
	}
}
