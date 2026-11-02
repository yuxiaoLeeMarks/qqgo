package qqmusic

import "testing"

// A song's cover comes from its album's picture MID: songs whose album QQ
// Music lists without a MID, as live recordings, showed no cover.
func TestTrackCover(t *testing.T) {
	for _, c := range []struct {
		name string
		json string
		want string
	}{
		{"picture of an album without a MID", `{"mid":"s1","title":"山雀","album":{"mid":"","pmid":"0030lak94GN5Ad_0"},"singer":[{"mid":"","name":"万能青年旅店"}]}`,
			"https://y.gtimg.cn/music/photo_new/T002R300x300M0000030lak94GN5Ad_0.jpg"},
		{"album's MID without a picture's", `{"mid":"s2","title":"夜曲","album":{"mid":"0024bjiL2aocxT"}}`,
			"https://y.gtimg.cn/music/photo_new/T002R300x300M0000024bjiL2aocxT.jpg"},
		{"the first artist with a photo", `{"mid":"s3","title":"Live","album":{"mid":""},"singer":[{"mid":"","name":"甲"},{"mid":"003GTrdR2M8Dvd","name":"乙"}]}`,
			"https://y.gtimg.cn/music/photo_new/T001R300x300M000003GTrdR2M8Dvd.jpg"},
	} {
		v, err := decode([]byte(c.json))
		if err != nil {
			t.Fatal(err)
		}
		tr, ok := track(v)
		if !ok || tr.Album.PicURL != c.want {
			t.Errorf("%s: cover %q, want %q", c.name, tr.Album.PicURL, c.want)
		}
	}
}
