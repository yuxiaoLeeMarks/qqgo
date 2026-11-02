package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// A radio playing, Now Playing's chip leaves it, which keeps the song
// playing and drops what the radio queued after it.
func TestExitRadio(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &qqmusic.Profile{UserID: 1, Nickname: "听众"}
	a.pl.Cue([]qqmusic.Track{{ID: 1, Name: "晚风"}, {ID: 2, Name: "推荐的歌"}})
	a.pl.AttachRadio("guess", func(got func([]qqmusic.Track)) {})
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText(radioLabel("guess")) {
		t.Fatalf("no chip in %q", tt.Texts())
	}
	if err := tt.Click("退出个性电台"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Radio() != "" || a.pl.Current().ID != 1 || len(a.pl.Queue()) != 1 {
		t.Fatalf("radio %q, current %v, queue %d", a.pl.Radio(), a.pl.Current(), len(a.pl.Queue()))
	}
}

// The radar goes on page by page, and a guess asks for more each time.
func TestRadioFeed(t *testing.T) {
	var pages []int
	radarTracks = func(page int) ([]qqmusic.Track, bool, error) {
		pages = append(pages, page)
		return []qqmusic.Track{{ID: int64(page), Name: "歌"}}, true, nil
	}
	defer func() { radarTracks = qqmusic.RadarTracks }()
	a := newTestApp()
	feed := a.radioFeed("radar")
	got := make(chan []qqmusic.Track, 2)
	// Without a window post drops what it is given: the feed is read from
	// its fetch.
	feed(func(tr []qqmusic.Track) { got <- tr })
	feed(func(tr []qqmusic.Track) { got <- tr })
	time.Sleep(100 * time.Millisecond)
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Errorf("asked for pages %v", pages)
	}
}
