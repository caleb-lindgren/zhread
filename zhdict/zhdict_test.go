package zhdict

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestSplitN(t *testing.T) {
	got := splitN([]byte("a\tb\tc\td\te"), '\t', 4)
	want := []string{"a", "b", "c", "d\te"}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
	short := splitN([]byte("a\tb"), '\t', 4)
	if string(short[1]) != "b" || short[2] != nil || short[3] != nil {
		t.Errorf("missing fields not nil: %q", short)
	}
}

func TestUnquote(t *testing.T) {
	cases := map[string]string{
		`plain`:                    "plain",
		`"class ""A""; top grade"`: `class "A"; top grade`,
		`"no inner quotes"`:        "no inner quotes",
		`a "quote" mid-field`:      `a "quote" mid-field`,
	}
	for in, want := range cases {
		if got := unquote([]byte(in)); got != want {
			t.Errorf("unquote(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanPleco(t *testing.T) {
	const mark = string(sectionMark)
	in := mark + "如：「甲」。" + mark + mark + "似：乙" + mark
	want := "如：「甲」。 ｜ 似：乙"
	if got := cleanPleco([]byte(in)); got != want {
		t.Errorf("cleanPleco = %q, want %q", got, want)
	}
	if got := cleanPleco([]byte("plain\x02def")); got != "plaindef" {
		t.Errorf("control character not dropped: %q", got)
	}
}

func TestHanRuns(t *testing.T) {
	// A comma joins, a Latin letter and a full stop split, and the
	// trailing joiner of a run is dropped with it.
	got := HanRuns("我愛A國，你好。天氣·晴，")
	want := []string{"我愛", "國，你好", "天氣·晴"}
	if len(got) != len(want) {
		t.Fatalf("got %d runs %q, want %d",
			len(got), got, len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("run %d = %q, want %q",
				i, string(got[i]), want[i])
		}
	}
	if n := len(HanRuns("hello, world")); n != 0 {
		t.Errorf("got %d runs for Latin input, want 0", n)
	}
}

func TestWidth(t *testing.T) {
	cases := map[string]int{
		"abc": 3,
		"中文":  4,
		"｜":   2,
		"dào": 3,
		"中a文": 5,
	}
	for in, want := range cases {
		if got := Width(in); got != want {
			t.Errorf("width(%q) = %d, want %d", in, got, want)
		}
	}
	if got := Pad("中", 6); got != "中    " {
		t.Errorf("pad(中, 6) = %q", got)
	}
}

func TestPickBidi(t *testing.T) {
	fewer := []string{"研究", "生命", "起源"}
	more := []string{"研究生", "命", "起源"}
	if got := pickBidi(more, fewer); !reflect.DeepEqual(got, fewer) {
		t.Errorf("fewer words not preferred: %q", got)
	}
	// Equal word counts, so the single-character count decides.
	a := []string{"甲乙", "丙丁"}
	b := []string{"甲", "乙丙丁"}
	if got := pickBidi(a, b); !reflect.DeepEqual(got, a) {
		t.Errorf("fewer single-character words not preferred: %q", got)
	}
}

// newTestProbe builds a probe over the real dictionaries, skipping the test
// where they are not present.
// testDict opens the dictionaries once for every test that needs them.
// needOptional skips a test whose expectations come from the optional
// files, when one is missing.
func needOptional(t *testing.T, d *Dict) {
	t.Helper()
	if m := d.MissingOptional(); len(m) > 0 {
		t.Skipf("optional files unavailable (make dict ARGS=\"--csld "+
			"--wikt\"): %s", strings.Join(m, ", "))
	}
}

func testDict(t *testing.T) *Dict {
	t.Helper()
	testDictOnce.Do(func() {
		var dir string
		if dir, testDictErr = FindDir(""); testDictErr == nil {
			testDictVal, testDictErr = Open(dir)
		}
	})
	if testDictErr != nil {
		t.Skipf("dictionaries unavailable: %v", testDictErr)
	}
	return testDictVal
}

var (
	testDictOnce sync.Once
	testDictVal  *Dict
	testDictErr  error
)

func newTestProbe(t *testing.T, input string) *Probe {
	t.Helper()
	p := NewProbe(HanRuns(input))
	p.Load(testDict(t))
	return p
}

// newTestFreqProbe is newTestProbe with the frequency table loaded, which
// -seg viterbi needs.
func newTestFreqProbe(t *testing.T, input string) *Probe {
	t.Helper()
	p := newTestProbe(t, input)
	if err := p.LoadFreq(testDict(t)); err != nil {
		t.Skipf("frequency table unavailable: %v", err)
	}
	return p
}

func TestSegment(t *testing.T) {
	cases := []struct {
		in   string
		mode string
		want string
	}{
		{"研究生命起源", SegFMM, "研究生/命/起源"},
		{"研究生命起源", SegViterbi, "研究/生命/起源"},
		{"研究生命起源", SegBMM, "研究/生命/起源"},
		{"研究生命起源", SegBidi, "研究/生命/起源"},
		{"我愛北京天安門", SegBidi, "我/愛/北京/天安門"},
		// Traditional and simplified input segment alike.
		{"我爱北京天安门", SegBidi, "我/爱/北京/天安门"},
		// Words carried only by the additions files.
		{"露鋒方筆館閣體", SegBidi, "露鋒/方筆/館閣體"},
		{"人臉識別醫患關係", SegBidi, "人臉識別/醫患關係"},
		// Punctuation delimits runs and is dropped.
		{"你好，世界！", SegBidi, "你好/世界"},
	}
	for _, c := range cases {
		p := newTestProbe(t, c.in)
		if c.mode == SegViterbi {
			p = newTestFreqProbe(t, c.in)
		}
		got := strings.Join(p.Segment(HanRuns(c.in), c.mode), "/")
		if got != c.want {
			t.Errorf("segment(%q, %s) = %q, want %q",
				c.in, c.mode, got, c.want)
		}
	}
}

func TestRows(t *testing.T) {
	// 𪚥 is in none of the four sources, so it exercises the notice.
	p := newTestProbe(t, "中文汪森𪚥")

	// Both sides have 中文, and the Chinese line comes first. The two
	// definitions distinguish the lines; the other three fields agree.
	g := p.Rows("中文")
	if g.Notice != "" {
		t.Fatalf("notice for a word the dictionaries have: %q",
			g.Notice)
	}
	if len(g.Rows) != 2 {
		t.Fatalf("got %d rows for 中文, want 2", len(g.Rows))
	}
	for _, r := range g.Rows {
		for i, f := range r {
			if f == "" {
				t.Errorf("row %q: field %d empty", r, i)
			}
		}
	}
	if !strings.HasPrefix(g.Rows[0][3], "特指漢族的語言及文字。") {
		t.Errorf("first line is not the Chinese definition: %q",
			g.Rows[0][3])
	}
	if g.Rows[1][3] != "Chinese language" {
		t.Errorf("second line is not the English definition: %q",
			g.Rows[1][3])
	}

	// 汪森 is in ccdict_additions.tsv and in no English source. The
	// missing English definition adds no line at all.
	g = p.Rows("汪森")
	if g.Notice != "" {
		t.Fatalf("notice for a word one dictionary has: %q", g.Notice)
	}
	if len(g.Rows) == 0 {
		t.Fatal("no rows for 汪森")
	}
	for _, r := range g.Rows {
		if r[0] != "汪森" || r[NFields-1] == "" {
			t.Errorf("row = %q, want a real 汪森 entry", r)
		}
	}

	// No source has 𪚥, so the group is the notice and nothing else.
	g = p.Rows("𪚥")
	if g.Rows != nil {
		t.Errorf("got %d rows for a word no source has: %q",
			len(g.Rows), g.Rows)
	}
	if want := noticeFor("𪚥"); g.Notice != want {
		t.Errorf("notice = %q, want %q", g.Notice, want)
	}
}

// TestRowOrder pins the ordering on synthetic entries, so it does not move
// when a dictionary does. The source indices are real ones, since that is
// what rows reads the language from, and the TSV layout is what
// entry.fields parses.
func TestRowOrder(t *testing.T) {
	const srcZh, srcEn = 1, 3
	if sources[srcZh].lang != LangZh || sources[srcZh].kind != kindTSV {
		t.Fatalf("source %d is not a Chinese TSV", srcZh)
	}
	if sources[srcEn].lang != LangEn || sources[srcEn].kind != kindTSV {
		t.Fatalf("source %d is not an English TSV", srcEn)
	}
	mk := func(src int32, def string) entry {
		return entry{line: []byte("中\t中\tzhōng\t" + def), src: src}
	}
	// One English definition is wider than one of the Chinese ones, so a
	// single sort over the whole group would interleave the languages.
	p := &Probe{hits: map[string][]entry{"中": {
		mk(srcZh, "短"),
		mk(srcEn, "a long English definition"),
		mk(srcZh, "一個比較長的中文解釋"),
		mk(srcEn, "short"),
	}}}
	var got []string
	for _, r := range p.Rows("中").Rows {
		got = append(got, r[NFields-1])
	}
	want := []string{
		"一個比較長的中文解釋",
		"短",
		"a long English definition",
		"short",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

// TestRenderNotice checks that a notice starts at column 0 and does not
// widen the columns the rows are padded into.
// TestLongHeadword covers the path in Probe.offer for headwords longer than
// maxCandRunes, which are never enumerated as candidates.
func TestLongHeadword(t *testing.T) {
	const long = "老吾老，以及人之老，幼吾幼，以及人之幼"
	runs := HanRuns(long)
	p := NewProbe(runs)
	p.Load(testDict(t))
	// The headword is longer than maxCandRunes, so it was never
	// enumerated as a candidate and can only be reached through the
	// substring test in Probe.longMatch.
	if len(p.Lookup(long)) == 0 {
		t.Errorf("long headword %q not matched", long)
	}
}

// TestViterbi covers what the frequency model buys over maximum matching.
// The first case is the one it was added for: both maximum matches give two
// words with one single character among them, so bidi's criteria both tie
// and it falls through to the backward result, which is the wrong one.
func TestViterbi(t *testing.T) {
	cases := []struct{ in, bidi, viterbi string }{
		// 流感 is a real but uncommon word; 交流 is a common one.
		{"交流感", "交/流感", "交流/感"},
		// 要是 and 竟是 are words, so the backward match swallows
		// the character that belongs to the word before them.
		{"這主要是我的問題", "這/主/要是/我/的/問題",
			"這/主要/是/我/的/問題"},
		{"他究竟是誰", "他/究/竟是/誰", "他/究竟/是/誰"},
		// The same three-character shape where the backward result
		// is the right one, so the counts have to agree with it
		// rather than reverse every such split.
		{"他來自己的家", "他/來/自己/的/家", "他/來/自己/的/家"},
	}
	for _, c := range cases {
		p := newTestFreqProbe(t, c.in)
		runs := HanRuns(c.in)
		got := strings.Join(p.Segment(runs, SegBidi), "/")
		if got != c.bidi {
			t.Errorf("bidi(%q) = %q, want %q", c.in, got, c.bidi)
		}
		got = strings.Join(p.Segment(runs, SegViterbi), "/")
		if got != c.viterbi {
			t.Errorf("viterbi(%q) = %q, want %q",
				c.in, got, c.viterbi)
		}
	}
}

// TestViterbiUnknownWords checks the unknownCount floor from both sides. A
// word only the additions files carry has no count, and has to survive
// anyway; a run no dictionary knows at all has to come apart into
// characters rather than be taken as one long word.
func TestViterbiUnknownWords(t *testing.T) {
	p := newTestFreqProbe(t, "露鋒方筆館閣體")
	got := strings.Join(p.Segment(HanRuns("露鋒方筆館閣體"), SegViterbi), "/")
	if want := "露鋒/方筆/館閣體"; got != want {
		t.Errorf("viterbi = %q, want %q", got, want)
	}
	for _, w := range []string{"露鋒", "方筆", "館閣體"} {
		if p.freq[w] != 0 {
			t.Errorf("%q has count %d; the case no longer "+
				"exercises the unknownCount floor",
				w, p.freq[w])
		}
	}
}

// TestViterbiJoiner checks that a joiner no word covers is dropped rather
// than emitted as a word of its own, as it is by fmm and bmm. Scoring it
// like a word would also bias the model against splitting there.
func TestViterbiJoiner(t *testing.T) {
	p := newTestFreqProbe(t, "國，你好")
	got := strings.Join(p.Segment(HanRuns("國，你好"), SegViterbi), "/")
	if want := "國/你好"; got != want {
		t.Errorf("viterbi = %q, want %q", got, want)
	}
	// A joiner inside a headword is kept, since a word covers it.
	p = newTestFreqProbe(t, "卡爾·馬克思")
	got = strings.Join(p.Segment(HanRuns("卡爾·馬克思"), SegViterbi), "/")
	if want := "卡爾·馬克思"; got != want {
		t.Errorf("viterbi = %q, want %q", got, want)
	}
}

// TestNoMatchTerminates covers maxTry's floor of one character. 𪚥 is in
// none of the sources, so maxRunes stays 0; without the floor every
// segmenter takes a zero-length word and loops forever.
func TestNoMatchTerminates(t *testing.T) {
	p := newTestProbe(t, "𪚥")
	if p.maxRunes != 0 {
		t.Fatalf("maxRunes = %d; 𪚥 now matches something and the "+
			"test no longer covers the empty-match case",
			p.maxRunes)
	}
	for _, mode := range []string{SegFMM, SegBMM, SegBidi} {
		got := p.Segment(HanRuns("𪚥"), mode)
		if len(got) != 1 || got[0] != "𪚥" {
			t.Errorf("segment(𪚥, %s) = %q, want [𪚥]",
				mode, got)
		}
	}
}

func TestFreqLine(t *testing.T) {
	cases := []struct {
		in    string
		word  string
		count int64
		ok    bool
	}{
		{"交流 5027 n", "交流", 5027, true},
		{"一 217830", "一", 217830, true}, // no POS tag
		{"", "", 0, false},
		{"交流", "", 0, false},     // no count
		{"交流 x n", "", 0, false}, // count not a number
	}
	for _, c := range cases {
		w, n, ok := freqLine([]byte(c.in))
		if w != c.word || n != c.count || ok != c.ok {
			t.Errorf("freqLine(%q) = %q, %d, %v; want %q, %d, %v",
				c.in, w, n, ok, c.word, c.count, c.ok)
		}
	}
}

// TestFreqLookup checks the traditional-to-simplified path: jieba's table is
// keyed on simplified headwords, and a traditional candidate finds its count
// through the simplified headword of an entry matched for it, with no
// conversion table involved.
func TestFreqLookup(t *testing.T) {
	p := newTestFreqProbe(t, "交流感學習")
	// 學習 is traditional and appears in the table only as 学习.
	if p.freq["學習"] == 0 {
		t.Error("no count for 學習 through its simplified headword")
	}
	if p.freq["交流"] <= p.freq["流感"] {
		t.Errorf("交流 (%d) is not commoner than 流感 (%d); the "+
			"交流感 case no longer turns on the counts",
			p.freq["交流"], p.freq["流感"])
	}
	if p.freqTotal <= 0 {
		t.Errorf("freqTotal = %d, want the sum of the table",
			p.freqTotal)
	}
}

func TestReadings(t *testing.T) {
	p := newTestProbe(t, "企業和模認識垃圾導播")
	needOptional(t, testDict(t))
	if err := p.LoadPron(testDict(t)); err != nil {
		t.Skipf("reading table unavailable: %v", err)
	}
	// readings of word's lines, as printed, keyed by definition prefix
	read := func(word, def string) string {
		t.Helper()
		for _, r := range p.Rows(word).Rows {
			if strings.HasPrefix(r[3], def) {
				return r[2]
			}
		}
		t.Fatalf("no line of %s starts %q", word, def)
		return ""
	}
	cases := []struct{ word, def, cn, tw string }{
		// Taiwan reading in MoEDict, Mainland in CC-CEDICT.
		{"企業", "從事生產", "qǐ yè (TW qì yè)", "qì yè (CN qǐ yè)"},
		{"企業", "company", "qǐ yè (TW qì yè)", "qì yè (CN qǐ yè)"},
		// MoEDict already gives the Mainland reading.
		{"認識", "① 曾經相識", "rèn shi (TW rèn shì)",
			"rèn shì (CN rèn shi)"},
		// hàn is Taiwan's alone; hé "sum" is hé on both sides, and
		// the surname is not the hé of "and".
		{"和", "<1>連之語音", "hé (TW hàn)", "hàn (CN hé)"},
		{"和", "◆ <名>", "hé", "hé"},
		{"和", "surname", "Hé", "Hé"},
		// mó is a Mainland reading too, so the row mó → mú, which is
		// about another sense, is not applied to it.
		{"模", "◆ <名>  ｜ ① 範式", "mó", "mó"},
		// A pair from Wiktionary, for a word the cross-strait
		// dictionary lacks.
		{"導播", "電視節目", "dǎo bō (TW dǎo bò)",
			"dǎo bò (CN dǎo bō)"},
		{"導播", "to direct", "dǎo bō (TW dǎo bò)",
			"dǎo bò (CN dǎo bō)"},
	}
	for _, pref := range []string{PronCN, PronTW} {
		p.PronPref = pref
		for _, c := range cases {
			want := c.cn
			if pref == PronTW {
				want = c.tw
			}
			if got := read(c.word, c.def); got != want {
				t.Errorf("-pron %s: %s %q: got %q, want %q",
					pref, c.word, c.def, got, want)
			}
		}
	}

	// CC-CEDICT's own note moves from the definition to the reading.
	p.PronPref = PronCN
	if got := read("垃圾", "trash"); got != "lā jī (TW lè sè)" {
		t.Errorf("垃圾: got reading %q", got)
	}
	for _, r := range p.Rows("垃圾").Rows {
		if strings.Contains(r[3], "Taiwan pr.") {
			t.Errorf("note left in definition: %q", r[3])
		}
	}
}

// TestReadingFallback covers words neither reading table has, where
// MoEDict's reading is taken for Taiwan's and CC-CEDICT's for the
// Mainland's.
func TestReadingFallback(t *testing.T) {
	d := testDict(t)
	needOptional(t, d)
	p := newTestProbe(t, "一字不识溜冰場閩語羧下咽薄伽丘標致")
	if err := p.LoadExtra(d); err != nil {
		t.Skipf("extra sources unavailable: %v", err)
	}
	if err := p.LoadPron(d); err != nil {
		t.Skipf("reading table unavailable: %v", err)
	}
	read := func(word, src, def string) string {
		t.Helper()
		g := p.Rows(word)
		for i, r := range g.Rows {
			if g.Source(i) == src && strings.HasPrefix(r[3], def) {
				return r[2]
			}
		}
		t.Fatalf("no %s line of %s starts %q", src, word, def)
		return ""
	}
	cases := []struct{ word, src, def, cn, tw string }{
		// Simplified input finds the traditional entries.
		{"一字不识", "MoE", "", "yī zì bù shí (TW yī zì bù shì)",
			"yī zì bù shì (CN yī zì bù shí)"},
		{"一字不识", "CC-CEDICT", "", "yī zì bù shí (TW yī zì bù shì)",
			"yī zì bù shì (CN yī zì bù shí)"},
		// The cross-strait dictionary has the word, with one reading.
		{"溜冰場", "CC-CEDICT", "", "liū bīng chǎng", "liū bīng chǎng"},
		// A proper noun: CC-CEDICT's capitals are kept.
		{"閩語", "MoE", "", "Mǐn yǔ (TW mín yǔ)",
			"mín yǔ (CN Mǐn yǔ)"},
		// A single character.
		{"羧", "CC-CEDICT", "", "suō", "suō"},
		// Two readings in CC-CEDICT, beautiful and Peugeot.
		{"標致", "CC-CEDICT", "(of a woman)", "biāo zhi", "biāo zhi"},
		// Two senses, listed in fallback_exclude.tsv.
		{"下咽", "CC-CEDICT", "(anatomy)", "xià yān", "xià yān"},
		// The Wiktionary table's row applies to neither reading.
		{"薄伽丘", "MoE", "", "Bó jiā qiū (TW bó qié qiū)",
			"bó qié qiū (CN Bó jiā qiū)"},
	}
	for _, pref := range []string{PronCN, PronTW} {
		p.PronPref = pref
		for _, c := range cases {
			want := c.cn
			if pref == PronTW {
				want = c.tw
			}
			if got := read(c.word, c.src, c.def); got != want {
				t.Errorf("-pron %s: %s %s: got %q, want %q",
					pref, c.word, c.src, got, want)
			}
		}
	}

	// Without the extra sources it cannot be told whether the
	// cross-strait dictionary has the word, so nothing is rewritten.
	q := newTestProbe(t, "一字不識")
	if err := q.LoadPron(d); err != nil {
		t.Skipf("reading table unavailable: %v", err)
	}
	q.PronPref = PronCN
	for _, r := range q.Rows("一字不識").Rows {
		if strings.Contains(r[2], "(TW") {
			t.Errorf("without LoadExtra: got %q", r[2])
		}
	}
}

func TestMoELines(t *testing.T) {
	def := "◆ <動>  ｜ ① 擊、敲擊。 ｜ 如：「打鐵趁熱」。 ｜ " +
		"唐．杜甫．詩：「打鼓發船何郡郎。」 ｜ 似：擊 ｜ ◆ <名> 量詞。"
	want := []MoELine{
		{MoEPos, "<動>"}, {MoENum, "① 擊、敲擊。"},
		{MoEExample, "如：「打鐵趁熱」。"},
		{MoECite, "唐．杜甫．詩：「打鼓發船何郡郎。」"},
		{MoERel, "似：擊"}, {MoEPos, "<名>"}, {MoEText, "量詞。"},
	}
	if got := MoELines(def); !slices.Equal(got, want) {
		t.Errorf("MoELines:\n got %v\nwant %v", got, want)
	}
	// A definition that quotes its source in passing is not a
	// quotation; one that ends with quotations has them split off; and
	// a sense number left inside a part starts a sense.
	def = "山丘和谿壑。語本漢書．敘傳上：「漁釣」。後比喻歸隱。 ｜ " +
		"乾食。詩經．伐木：「民之失德」。莊子：「乾餱」 ｜ ① 承載著人。 ② 所載的人。"
	want = []MoELine{
		{MoEText, "山丘和谿壑。語本漢書．敘傳上：「漁釣」。後比喻歸隱。"},
		{MoEText, "乾食。"}, {MoECite, "詩經．伐木：「民之失德」。"},
		{MoECite, "莊子：「乾餱」"},
		{MoENum, "① 承載著人。"}, {MoENum, "② 所載的人。"},
	}
	if got := MoELines(def); !slices.Equal(got, want) {
		t.Errorf("MoELines:\n got %v\nwant %v", got, want)
	}
	if got := Preferred("qǐ yè (TW qì yè)"); got != "qǐ yè" {
		t.Errorf("Preferred %q", got)
	}
	const v = "yī diǎnr (變)yì diǎr"
	if got := Preferred(v); got != v {
		t.Errorf("Preferred kept no variant: %q", got)
	}
}

func TestMoESenses(t *testing.T) {
	def := "◆ <動>  ｜ ① 擊。 ｜ 如：「打鐵」。 ｜ 唐．杜甫．詩：「打鼓。」" +
		" ｜ ② 攻擊。 ｜ ◆ <名> 量詞。 ｜ 似：擊"
	got := MoESenses(def)
	want := []Sense{
		{Pos: "<動>", Text: "① 擊。", Ex: []string{"如：「打鐵」。"},
			Cites: []string{"唐．杜甫．詩：「打鼓。」"}},
		{Pos: "<動>", Text: "② 攻擊。"},
		{Pos: "<名>", Text: "量詞。", Rels: []string{"似：擊"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MoESenses:\n got %q\nwant %q", got, want)
	}

	def = "<助>  ｜ ① 結構助詞： ｜ ⑴ 置於形容詞後。如：「美麗的風景」。 ｜ " +
		"⑵置於名詞後。 ｜ ② 句尾助詞：置於句尾。 ｜ 如：「是的！」"
	got = MoESenses(def)
	want = []Sense{
		{Pos: "<助>", Heads: []string{"① 結構助詞："}, Text: "(1) 置於形容詞後。",
			Ex: []string{"如：「美麗的風景」。"}},
		{Pos: "<助>", Heads: []string{"① 結構助詞："}, Text: "(2) 置於名詞後。"},
		{Pos: "<助>", Text: "② 句尾助詞：置於句尾。",
			Ex: []string{"如：「是的！」"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MoESenses, sub-senses:\n got %q\nwant %q", got, want)
	}
}

func TestCSLDSenses(t *testing.T) {
	def := "★¶1.擊打：¶(1)擊打。¶如：「打鐘」、「捶打」。¶(2)被擊打。" +
		"¶2.〈書〉遷延。¶如：「須臾萬變」。¶引：足下得須臾（《史記》）。" +
		"¶助詞：¶1.表完成。¶2.用在：¶如：「走了」。"
	got := CSLDSenses(def)
	want := []Sense{
		{Pos: "〈大陸〉", Heads: []string{"1.擊打："}, Text: "(1)擊打。",
			Ex: []string{"如：「打鐘」、「捶打」。"}},
		{Heads: []string{"1.擊打："}, Text: "(2)被擊打。"},
		{Text: "2.〈書〉遷延。", Ex: []string{"如：「須臾萬變」。"},
			Cites: []string{"足下得須臾（《史記》）。"}},
		{Pos: "助詞：", Text: "1.表完成。"},
		// Not a heading: examples follow it.
		{Pos: "助詞：", Text: "2.用在：", Ex: []string{"如：「走了」。"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CSLDSenses:\n got %q\nwant %q", got, want)
	}

	// Headings nest by numbering; a long one is a heading, not a Pos;
	// and an example left in the text is split off.
	def = "標點符號之一。形式為「；」：¶(1)用於分開句子。例如：¶a)鯨魚是獸類。" +
		"¶b)語言。¶(2)用於複句。例如：年滿十八周歲。¶古代人臣中最高的三個官職：¶1.太師。"
	got = CSLDSenses(def)
	h := "標點符號之一。形式為「；」："
	want = []Sense{
		{Heads: []string{h, "(1)用於分開句子。例如："}, Text: "a)鯨魚是獸類。"},
		{Heads: []string{h, "(1)用於分開句子。例如："}, Text: "b)語言。"},
		{Heads: []string{h}, Text: "(2)用於複句。",
			Ex: []string{"例如：年滿十八周歲。"}},
		{Heads: []string{"古代人臣中最高的三個官職："}, Text: "1.太師。"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CSLDSenses, nested:\n got %q\nwant %q", got, want)
	}
}

func TestWiktSenses(t *testing.T) {
	def := "<verb>¶to hit¶例：打人 to hit people¶引：打鼓 (Du Fu)¶to fight"
	got := WiktSenses(def)
	want := []Sense{
		{Pos: "<verb>", Text: "to hit",
			Ex:    []string{"例：打人 to hit people"},
			Cites: []string{"引：打鼓 (Du Fu)"}},
		{Pos: "<verb>", Text: "to fight"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WiktSenses:\n got %q\nwant %q", got, want)
	}
	if !pointerOnly("variant of 個|个[gè]; CL:個|个[gè]") ||
		pointerOnly("to hit; variant of 個|个[gè]") {
		t.Errorf("pointerOnly")
	}
}

// TestVariants checks that variant entries fold into their standard form:
// 暖's CC-CEDICT entries 煖, 㬉 and 煗 are only variants of it, and 卷起 is a
// variant of 捲起 in traditional text but its own simplified form.
func TestVariants(t *testing.T) {
	en := func(g Group) (defs, heads, vars []string) {
		for i := g.Zh; i < len(g.Rows); i++ {
			defs = append(defs, g.Rows[i][NFields-1])
			heads = append(heads, g.Rows[i][0])
			vars = append(vars, g.Variant[i])
		}
		return
	}
	for _, c := range []struct {
		text, word string
		trad       bool
		defs       []string
		heads      []string
		vars       []string
	}{
		{"暖", "暖", false, []string{"warm; to warm"}, []string{"暖"},
			[]string{""}},
		{"煖", "煖", true, []string{"warm; to warm"}, []string{"暖"},
			[]string{"煖"}},
		{"卷起", "卷起", false, nil, []string{"捲起"}, []string{""}},
		{"卷起", "卷起", true, nil, []string{"捲起"}, []string{"卷起"}},
	} {
		p := newTestProbe(t, c.text)
		p.Trad = c.trad
		if err := p.LoadTargets(testDict(t)); err != nil {
			t.Fatal(err)
		}
		defs, heads, vars := en(p.Rows(c.word))
		for _, d := range defs {
			if strings.Contains(d, "variant of") {
				t.Errorf("%s (trad %v): variant row kept: %q",
					c.word, c.trad, defs)
			}
		}
		if c.defs != nil && !reflect.DeepEqual(defs, c.defs) {
			t.Errorf("%s (trad %v): defs = %q, want %q", c.word,
				c.trad, defs, c.defs)
		}
		if !reflect.DeepEqual(heads, c.heads) ||
			!reflect.DeepEqual(vars, c.vars) {
			t.Errorf("%s (trad %v): heads %q variants %q, "+
				"want %q %q", c.word, c.trad, heads, vars,
				c.heads, c.vars)
		}
	}
}

// markup matches HTML left in a definition: 兩岸詞典 has tables, <sup> and
// <sub> in its cells, which parse_csld_defs.py turns into text, and any that
// slipped through, from it or from another source, would show as raw tags.
var markup = regexp.MustCompile(`(?i)</?(?:table|tr|td|th|br|sup|sub|` +
	`u|b|i|font|span|div|p|a|img|em|strong|small|ruby|rt|hr|li|ul|ol)` +
	`\b[^>]*>|&(?:[a-z]{2,8};|nbsp|amp|lt|gt|quot|sup[123]|#[0-9]+|` +
	`#x[0-9a-fA-F]+)`)

// TestNoMarkup checks every line of every source for HTML.
func TestNoMarkup(t *testing.T) {
	d := testDict(t)
	extra, err := d.extraBufs()
	if err != nil {
		t.Skipf("extra sources unavailable: %v", err)
	}
	for si, s := range sources {
		buf := d.bufs[si]
		if s.extra {
			buf = extra[si]
		}
		n := 0
		for _, line := range strings.Split(string(buf), "\n") {
			if m := markup.FindString(line); m != "" {
				if n++; n <= 10 {
					head, _, _ := strings.Cut(line, "\t")
					t.Errorf("%s: %s has %q", s.path,
						head, m)
				}
			}
		}
		if n > 10 {
			t.Errorf("%s: %d lines with markup in all", s.path, n)
		}
	}
}

func TestZhuyin(t *testing.T) {
	for py, want := range map[string]string{
		"zǔ hé":       "ㄗㄨˇ ㄏㄜˊ",
		"zāng xī xī":  "ㄗㄤ ㄒㄧ ㄒㄧ",
		"tóu fa":      "ㄊㄡˊ ˙ㄈㄚ",
		"zhī chí shì": "ㄓ ㄔˊ ㄕˋ",
		"yī diǎnr":    "ㄧ ㄉㄧㄢˇㄦ",
		"yǒu yuè":     "ㄧㄡˇ ㄩㄝˋ",
		"wèi wēng":    "ㄨㄟˋ ㄨㄥ",
		"jué qù xū":   "ㄐㄩㄝˊ ㄑㄩˋ ㄒㄩ",
		"lǜ nǚ":       "ㄌㄩˋ ㄋㄩˇ",
		"Zhōng guó":   "ㄓㄨㄥ ㄍㄨㄛˊ",
		"yìng yín èr": "ㄧㄥˋ ㄧㄣˊ ㄦˋ",
		"xiōng":       "ㄒㄩㄥ",
		"a , ba":      "˙ㄚ , ˙ㄅㄚ",
		"lu:è nu:3":   "ㄌㄩㄝˋ ㄋㄩˇ",
		"wán r":       "ㄨㄢˊ ㄦ",
		"hm":          "",
	} {
		if got := Zhuyin(py); got != want {
			t.Errorf("Zhuyin(%q) = %q, want %q", py, got, want)
		}
	}
}

// TestPointerHidden checks that an English definition that only points
// elsewhere is left out when another dictionary defines the word:
// CC-CEDICT has 山西 only as "see 山西省[Shān xī Shěng]", and Wiktionary
// defines it. Without Wiktionary the see row is all there is, and stays.
func TestPointerHidden(t *testing.T) {
	en := func(g Group) (defs []string) {
		for i := g.Zh; i < len(g.Rows); i++ {
			defs = append(defs, g.Rows[i][NFields-1])
		}
		return
	}
	p := newTestProbe(t, "山西")
	if d := en(p.Rows("山西")); len(d) != 1 || !pointerOnly(d[0]) {
		t.Errorf("山西 without the extras: English %q, want the "+
			"see row alone", d)
	}
	if err := p.LoadExtra(testDict(t)); err != nil {
		t.Skipf("extra sources unavailable: %v", err)
	}
	d := en(p.Rows("山西"))
	if len(d) == 0 {
		t.Fatalf("山西: no English")
	}
	for _, def := range d {
		if pointerOnly(def) {
			t.Errorf("山西: see row kept: %q", d)
		}
	}
}
