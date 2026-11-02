package qqmusic

import "testing"

// Covers ask for a size the image CDN has: it has none past 500 of a
// chart's or an album's, where a larger one showed nothing.
func TestSized(t *testing.T) {
	for in, want := range map[string]string{
		"http://y.gtimg.cn/music/photo_new/T003R300x300M000000wHWc22fX2z1.jpg":  "https://y.gtimg.cn/music/photo_new/T003R500x500M000000wHWc22fX2z1.jpg",
		"https://y.gtimg.cn/music/photo_new/T002R300x300M0000024bjiL2aocxT.jpg": "https://y.gtimg.cn/music/photo_new/T002R500x500M0000024bjiL2aocxT.jpg",
		"https://qpic.y.qq.com/music_cover/abc/300?n=1":                         "https://qpic.y.qq.com/music_cover/abc/600?n=1",
		"": "",
	} {
		if got := Sized(in, 600); got != want {
			t.Errorf("Sized(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Sized("https://y.gtimg.cn/music/photo_new/T002R300x300M000x.jpg", 96); got != "https://y.gtimg.cn/music/photo_new/T002R150x150M000x.jpg" {
		t.Errorf("small: %s", got)
	}
}
