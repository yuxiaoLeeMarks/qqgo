package player

import (
	"os"
	"testing"
	"time"

	"qqgo/internal/qqmusic"
)

// A free song streams from QQ Music's CDN, and seeks into what has not
// come in yet.
func TestStreamLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip()
	}
	songs, err := qqmusic.Search("不及我", 10)
	if err != nil {
		t.Fatal(err)
	}
	var tr *qqmusic.Track
	for i := range songs {
		if !songs[i].VIP {
			tr = &songs[i]
			break
		}
	}
	if tr == nil {
		t.Skip("no free song found")
	}
	st, err := qqmusic.SongURL(*tr, "high")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	dl := startDownload(st.URLs(), st.Size, nil, nil)
	s, err := openMP3Stream(dl, tr.Duration)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("opened in", time.Since(start), s.rate, s.frames, s.start)
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	t.Log("read", n, err)
	if err := s.SetPosition(int64(s.rate) * 120); err != nil {
		t.Fatal(err, dl.n, dl.done, dl.err)
	}
	total := 0
	for i := 0; i < 200; i++ {
		n, err := s.Read(buf)
		total += n
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Log("after seek read", total, "in", time.Since(start), "progress", dl.Progress())
	b, ok := s.spec.at(int64(s.rate)*120 + 20000)
	t.Log(ok, b)
	s.Close()
}
