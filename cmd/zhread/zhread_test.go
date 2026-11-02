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
	keys(v, "gh")
	if !onScreen(s, "pipe the selection") || onScreen(s, "enterprise") {
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
// Chinese, MoEDict as well on C; and that x shows quotations, which are
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
		t.Errorf("MoEDict shown before C")
	}
	keys(v, "C")
	if p := paneText(v); !strings.Contains(p, "MoE") ||
		!strings.Contains(p, "① 美、善，理想的。 如：「好東西」") {
		t.Errorf("no MoEDict after C:\n%s", p)
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
	keys(v, "lC")
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
// MoEDict's only after C, and that a card carries quotations only when x
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
	keys(v, "⎋C/好⏎vl⏎")
	if count("1.美善的") != 1 || count("① 美、善") != 1 {
		t.Errorf("MoEDict's senses not offered after C")
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
// and is shown beside CC-CEDICT, labelled, only after E.
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
	if p := paneText(v); !strings.Contains(p, "Wiktionary  <noun>") {
		t.Errorf("土豆 after E:\n%s", p)
	}
}

func TestClick(t *testing.T) {
	v, _ := newTestView(t, book)
	// Row 2 is line 2; each Han character is two columns wide, so
	// column 7 is the right half of 天, the fourth character.
	v.mouse(tcell.NewEventMouse(7, 2, tcell.Button1, tcell.ModNone))
	if v.cl != 2 || v.cc != 3 {
		t.Fatalf("click: at %d:%d, want 2:3", v.cl, v.cc)
	}
	// a click below the text does nothing
	v.draw()
	v.mouse(tcell.NewEventMouse(0, v.textH(), tcell.Button1,
		tcell.ModNone))
	if v.cl != 2 || v.cc != 3 {
		t.Errorf("click on the pane: at %d:%d", v.cl, v.cc)
	}
}

func TestVisualNoWordHighlight(t *testing.T) {
	v, s := newTestView(t, book)
	keys(v, "jjw") // on 今天
	cells, w, _ := s.GetContents()
	// 天, after the cursor on 今, is in the word's colours
	if st := cells[2*w+6].Style; st != stCurWord {
		t.Fatalf("normal: rest of the word not highlighted")
	}
	keys(v, "v")
	cells, w, _ = s.GetContents()
	if st := cells[2*w+6].Style; st != stDef {
		t.Errorf("visual: rest of the word still highlighted")
	}
}
