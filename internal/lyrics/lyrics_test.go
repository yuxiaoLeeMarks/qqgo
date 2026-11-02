package lyrics

import (
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	lines := mergeLyrics("[00:01.000] 作词 : 某人\n[00:30.54]故事的小黄花\n[00:34.165][01:10.1]从出生那年就飘着\n[00:40.00]\n",
		"[00:30.54]The little yellow flower\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[1].At != 30540*time.Millisecond || lines[1].Translation != "The little yellow flower" {
		t.Errorf("line 1: %+v", lines[1])
	}
	if lines[3].At != 70100*time.Millisecond || lines[3].Text != "从出生那年就飘着" {
		t.Errorf("line 3: %+v", lines[3])
	}
	if i := Index(lines, 33*time.Second); i != 1 {
		t.Errorf("index at 33s = %d", i)
	}
	if i := Index(lines, 0); i != -1 {
		t.Errorf("index at 0 = %d", i)
	}
}

func TestParseQRC(t *testing.T) {
	qrc := `<?xml version="1.0" encoding="utf-8"?>
<QrcInfos><QrcHeadInfo SaveTime="1" Version="100"/><LyricInfo LyricCount="1"><Lyric_1 LyricType="1" LyricContent="[ti:晴天]
[0,1200]晴天(0,600) - (600,100)周杰伦(700,500)
[13010,4780]从(13010,660)不(13670,670) (14340,30)主(14370,480)
[17790,1770]I(17790,170) (17960,450)love (Live)(18410,290)
"/></LyricInfo></QrcInfos>`
	lines := Build(qrc, "[00:13.00]Never\n[00:17.80]我爱\n[00:20.00]//")
	if len(lines) != 3 || !lines[1].Timed {
		t.Fatalf("lines %+v", lines)
	}
	l := lines[1]
	if l.Text != "从不 主" || l.At != 13010*time.Millisecond || l.End != 17790*time.Millisecond {
		t.Errorf("line %q %v-%v", l.Text, l.At, l.End)
	}
	if len(l.Words) != 3 || l.Words[1].Text != "不 " || l.Words[2].Start != 14370*time.Millisecond {
		t.Errorf("words %+v", l.Words)
	}
	if lines[2].Text != "I love (Live)" || lines[2].Words[1].Text != "love (Live)" {
		t.Errorf("a word with brackets of its own: %q %+v", lines[2].Text, lines[2].Words)
	}
	if l.Translation != "Never" || lines[2].Translation != "我爱" {
		t.Errorf("translations %q %q", l.Translation, lines[2].Translation)
	}
}

// Lyrics whose XML has an unescaped quote inside still read in full: the
// strict reading stopped at it.
func TestQRCLooseQuote(t *testing.T) {
	qrc := `<QrcInfos><LyricInfo><Lyric_1 LyricContent="[0,500]Marcus "MarcLo" Lomax(0,500)
[500,500]second(500,500)
[1000,500]third &amp; last(1000,500)"/></LyricInfo></QrcInfos>`
	lines := Build(qrc, "")
	if len(lines) != 3 || lines[0].Text != `Marcus "MarcLo" Lomax` || lines[2].Text != "third & last" {
		t.Fatalf("lines %+v", lines)
	}
}

func TestSpreadWords(t *testing.T) {
	w := SpreadWords("我爱 you all", 0, 10*time.Second)
	got := []string{}
	for _, x := range w {
		got = append(got, x.Text)
	}
	if len(w) != 4 || got[0] != "我" || got[1] != "爱" || got[2] != "you " || got[3] != "all" {
		t.Fatalf("words %q", got)
	}
	if w[0].Start != 0 || w[3].End > 10*time.Second || w[1].Start != w[0].End {
		t.Errorf("times %+v", w)
	}
}
