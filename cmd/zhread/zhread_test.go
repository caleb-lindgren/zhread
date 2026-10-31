package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caleb-lindgren/ankitools/internal/zhdict"
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
	got := mark(ctx, 0, 1, false)
	if got != "<b>好</b>人有好報&lt;b&gt;" {
		t.Errorf("context: %s", got)
	}
	blank := `<span class=cloze>[…]</span>`
	want := blank + "人有" + blank + "報&lt;b&gt;"
	if got := mark(ctx, 0, 1, true); got != want {
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
	v := &view{s: s, d: d, dict: dict, rec: rec,
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

func TestPane(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "/企业⏎")
	if !onScreen(s, "企业  qǐ yè") || onScreen(s, "TW qì yè") {
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
	keys(v, "g?")
	if !onScreen(s, "pipe the selection") || onScreen(s, "enterprise") {
		t.Errorf("g? does not show the keys:\n%s",
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
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
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
		7:  "后来他把<span class=cloze>[…]</span>剪短了，看起来很精神。",
		8:  "後來他把<b>頭髮</b>剪短了，看起來很精神。",
		9:  "後來他把<span class=cloze>[…]</span>剪短了，看起來很精神。",
		10: "book.txt:3",
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

	// The same word again is a duplicate: the first Enter warns, the
	// second saves.
	keys(v, "/头发⏎vl⏎⏎")
	if v.mode != card || !onScreen(s, "save anyway") {
		t.Fatalf("no duplicate warning:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "⎋")
	if n := len(v.rec.cards); n != 1 {
		t.Errorf("%d cards after cancelling, want 1", n)
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
	if !onScreen(s, "New card: 讨论") || !onScreen(s, "(1 of 3)") {
		t.Errorf("first card:\n%s", strings.Join(screen(s), "\n"))
	}
	keys(v, "⏎n") // save 讨论, skip 了
	if !onScreen(s, "New card: 企业") {
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

// TestQuotations checks that x shows MoEDict's quotations, which are
// hidden at first, while examples are always on the definition's line.
func TestQuotations(t *testing.T) {
	v, s := newTestView(t, "他好高兴。\n")
	s.SetSize(60, 40)
	v.rows = nil
	keys(v, "l")
	if !onScreen(s, "① 美、善，理想的。 如：「好東西」") {
		t.Errorf("example not on the sense's line:\n%s",
			strings.Join(screen(s), "\n"))
	}
	if onScreen(s, "唐．韋莊") {
		t.Errorf("quotation shown before x")
	}
	keys(v, "x")
	if !onScreen(s, "唐．韋莊") {
		t.Errorf("no quotation after x:\n%s",
			strings.Join(screen(s), "\n"))
	}
	// The entry is longer than the pane: J scrolls it.
	if !onScreen(s, "↓") {
		t.Fatalf("no note of rows below:\n%s",
			strings.Join(screen(s), "\n"))
	}
	keys(v, "J")
	if v.pscroll != 1 || !onScreen(s, "↑1") {
		t.Errorf("J: scrolled %d", v.pscroll)
	}
	keys(v, "l")
	if v.pscroll != 0 {
		t.Errorf("scroll kept for the next word: %d", v.pscroll)
	}
}

// TestCardSenses checks that a MoEDict definition is offered a sense at a
// time, and that a card carries quotations only when x showed them.
func TestCardSenses(t *testing.T) {
	v, s := newTestView(t, "他好高兴。\n")
	keys(v, "lvl⏎")
	if v.mode != card {
		t.Fatalf("mode %d", v.mode)
	}
	n := 0
	for _, o := range v.dr.opts[:v.dr.zh] {
		if strings.HasPrefix(o.s.Text, "① 美、善") {
			n++
		}
	}
	if n != 1 || v.dr.zh < 10 {
		t.Fatalf("%d options for 好, %d with sense ①", v.dr.zh, n)
	}
	// The list scrolls to keep the cursor's option in view.
	s.SetSize(60, 20)
	v.rows = nil
	keys(v, strings.Repeat("j", v.dr.zh-1))
	if txt := v.dr.opts[v.dr.cur].s.Text; !onScreen(s, txt) {
		t.Errorf("option %d (%s) not in view:\n%s", v.dr.cur, txt,
			strings.Join(screen(s), "\n"))
	}
	f := v.dr.forms(v.conv)
	cur := 0
	for i, o := range v.dr.opts {
		if len(o.s.Cites) > 0 {
			cur = i
			break
		}
	}
	v.dr.sel[cur] = true
	if note := v.dr.note(f, false, false); strings.Contains(note[4],
		v.dr.opts[cur].s.Cites[0]) {
		t.Errorf("quotation on a card made without x")
	}
	if note := v.dr.note(f, false, true); !strings.Contains(note[4],
		v.dr.opts[cur].s.Cites[0]) {
		t.Errorf("no quotation on a card made with x")
	}
}
