package opencc

import "testing"

func TestConvert(t *testing.T) {
	cases := []struct{ conv, in, want string }{
		// One simplified character, several traditional ones: the
		// phrase decides.
		{"s2tw", "头发", "頭髮"},
		{"s2tw", "发现", "發現"},
		{"s2tw", "以后", "以後"},
		{"s2tw", "皇后", "皇后"},
		{"s2tw", "干燥", "乾燥"},
		{"s2tw", "干部", "幹部"},
		// The Taiwan variant stage.
		{"s2tw", "里面", "裡面"},
		{"tw2s", "頭髮裡面", "头发里面"},
		{"tw2s", "abc 123", "abc 123"},
	}
	convs := map[string]*Converter{}
	for _, c := range cases {
		conv := convs[c.conv]
		if conv == nil {
			var err error
			conv, err = Load("../../dict/opencc", c.conv)
			if err != nil {
				t.Skipf("tables unavailable: %v", err)
			}
			convs[c.conv] = conv
		}
		if got := conv.Convert(c.in); got != c.want {
			t.Errorf("%s(%s) = %s, want %s", c.conv, c.in, got,
				c.want)
		}
	}
}
