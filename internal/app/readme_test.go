package app

import (
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/lyrics"
	"qqgo/internal/qqmusic"
)

// With NM_FRAMES and LIVE set, saves the screenshots of the README into
// NM_FRAMES, of what QQ Music has now, as someone signed out sees it:
//
//	NM_FRAMES=docs/screenshots LIVE=1 go test -run TestReadmeShots ./internal/app/
//
// The views are the app's, drawn without a window; the covers load as the
// app loads them, their results run here in place of the main thread.
func TestReadmeShots(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" || os.Getenv("LIVE") == "" {
		t.Skip("set NM_FRAMES and LIVE to save the README's screenshots")
	}
	posts := make(chan func(), 1024)
	a := newTestApp()
	a.settings = defaultSettings()
	a.art = newArtCache(func(fn func()) { posts <- fn })

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	a.featured, err = qqmusic.FeaturedPlaylists(30)
	must(err)
	a.charts, err = qqmusic.Charts()
	must(err)
	a.daily, err = qqmusic.DailySongs()
	must(err)
	a.homeAt = time.Now()

	// Songs anyone may play, of a few bands, from albums of their own for
	// covers of their own on the wall: the first, 春日影, comes with its
	// translation.
	var queue []qqmusic.Track
	for _, q := range []string{"春日影 MyGO", "Ave Mujica", "告五人", "草东没有派对", "万能青年旅店"} {
		songs, err := qqmusic.Search(q, 30)
		must(err)
		n := 0
		for _, s := range songs {
			if !s.VIP && s.Album.MID != "" && n < 4 {
				queue, n = append(queue, s), n+1
			}
		}
	}
	lyric, trans, err := qqmusic.Lyrics(queue[0])
	must(err)
	lines := lyrics.Build(lyric, trans)
	// A line a little into the song, with its translation.
	at := time.Duration(0)
	for _, l := range lines {
		if l.At > 40*time.Second && l.Translation != "" {
			at = l.At + (l.End-l.At)/2
			break
		}
	}

	ar, err := qqmusic.ArtistDetail("003GTrdR2M8Dvd") // 万能青年旅店
	must(err)
	a.artists[ar.MID] = &artistData{profile: ar}
	search := route{page: pageSearch, query: "万能青年旅店", kind: qqmusic.SearchSongs}
	res, err := qqmusic.SearchPage(search.query, search.kind, searchLimit, 0)
	must(err)
	a.searches[search] = &searchData{res: res, pages: 1, page: paged{loaded: true, more: true}}

	a.volume = 0.7
	a.pl.SetVolume(a.volume)
	a.pl.Cue(queue)
	a.pl.SetLyrics(lines)
	a.pl.Hold(at)

	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetScale(1.5)
	tt.SetDark(true)
	// settle draws until the covers the frames ask for have all loaded.
	settle := func() {
		for range 30 {
			tt.Advance(500 * time.Millisecond)
			tt.Frame()
			select {
			case fn := <-posts:
				fn()
			case <-time.After(3 * time.Second):
				return
			}
			for done := false; !done; {
				select {
				case fn := <-posts:
					fn()
				case <-time.After(300 * time.Millisecond):
					done = true
				}
			}
		}
	}
	save := func(name string) {
		t.Helper()
		settle()
		tt.Advance(2 * time.Second)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".jpg"))
		must(err)
		defer f.Close()
		must(jpeg.Encode(f, tt.Image(), &jpeg.Options{Quality: 90}))
	}

	a.open(route{page: pageHome})
	save("home")
	tt.SetDark(false)
	save("home-light")
	tt.SetDark(true)
	a.open(route{page: pageArtist, mid: ar.MID})
	save("artist")
	a.open(search)
	save("search")

	a.nowPlaying, a.npPane = true, paneLyrics
	save("lyrics")
	a.npPane = paneStage
	save("stage")
	a.npPane = paneWall
	save("wall")
}
