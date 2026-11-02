package qqmusic

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// These call QQ Music, as anyone signed out: they skip unless LIVE is set.

func live(t *testing.T) {
	t.Helper()
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE to call QQ Music")
	}
}

// The charts list, and a chart's songs load in full, with their files.
func TestLiveCharts(t *testing.T) {
	live(t)
	charts, err := Charts()
	if err != nil || len(charts) < 4 {
		t.Fatalf("Charts: %d, %v", len(charts), err)
	}
	if charts[0].ID >= 0 || len(charts[0].Preview) == 0 || charts[0].Cover() == "" {
		t.Errorf("first chart: %+v", charts[0])
	}
	pl, err := PlaylistDetail(charts[0].ID)
	if err != nil {
		t.Fatalf("PlaylistDetail(%d): %v", charts[0].ID, err)
	}
	if len(pl.Tracks) < 50 || pl.Tracks[0].MID == "" || len(pl.Tracks[0].Sizes) == 0 {
		t.Errorf("chart %q: %d tracks, first %+v", pl.Name, len(pl.Tracks), pl.Tracks[0])
	}
}

// A search finds songs, artists, albums and playlists, and pages on.
func TestLiveSearch(t *testing.T) {
	live(t)
	for _, kind := range []SearchType{SearchSongs, SearchArtists, SearchAlbums, SearchPlaylists} {
		r, err := SearchPage("周杰伦", kind, 20, 0)
		if err != nil || r.Len() == 0 || r.Total == 0 {
			t.Fatalf("search %d: %d of %d, %v", kind, r.Len(), r.Total, err)
		}
	}
	first, _ := SearchPage("晴天", SearchSongs, 10, 0)
	second, err := SearchPage("晴天", SearchSongs, 10, 10)
	if err != nil || len(second.Songs) == 0 || second.Songs[0].MID == first.Songs[0].MID {
		t.Errorf("second page: %v", err)
	}
	s, err := SearchSuggest("周杰")
	if err != nil || len(s.Songs) == 0 || len(s.Artists) == 0 || len(s.Order) != 3 {
		t.Errorf("suggest: %+v, %v", s, err)
	}
}

// An album, an artist, and their lists load.
func TestLiveAlbumArtist(t *testing.T) {
	live(t)
	al, err := AlbumDetail("0024bjiL2aocxT")
	if err != nil || al.Name == "" || len(al.Songs) < 10 || len(al.Artists) == 0 || al.PublishTime == 0 {
		t.Fatalf("album: %+v, %v", al, err)
	}
	ar, err := ArtistDetail("0025NhlN2yWrP4")
	if err != nil || ar.Name != "周杰伦" || len(ar.Songs) < 20 || ar.AlbumSize == 0 || ar.MusicSize == 0 {
		t.Fatalf("artist: %+v, %v", ar, err)
	}
	latest, err := ArtistLatestSongs("0025NhlN2yWrP4", 120)
	if err != nil || len(latest) != 120 {
		t.Errorf("latest: %d, %v", len(latest), err)
	}
	albums, more, err := ArtistAlbums("0025NhlN2yWrP4", 10, 0)
	if err != nil || len(albums) != 10 || !more {
		t.Errorf("albums: %d %v %v", len(albums), more, err)
	}
}

// A playlist loads past its first page.
func TestLivePlaylist(t *testing.T) {
	live(t)
	featured, err := FeaturedPlaylists(10)
	if err != nil || len(featured) == 0 {
		t.Fatalf("featured: %d, %v", len(featured), err)
	}
	pl, err := PlaylistDetail(7039749142)
	if err != nil || pl.Name == "" || len(pl.Tracks) < 90 || pl.Creator.Nickname == "" {
		t.Fatalf("playlist: %v", err)
	}
	// A long playlist loads past its first page, up to maxTracks.
	for _, f := range featured {
		if f.TrackCount <= 300 {
			continue
		}
		long, err := PlaylistDetail(f.ID)
		if err != nil || len(long.Tracks) < 250 || len(long.Tracks) > maxTracks {
			t.Errorf("%q of %d: loaded %d, %v", f.Name, f.TrackCount, len(long.Tracks), err)
		}
		break
	}
	daily, err := DailySongs()
	if err != nil || len(daily) != 30 {
		t.Errorf("daily: %d, %v", len(daily), err)
	}
}

// A free song streams, and its lyrics come word-timed.
func TestLiveStreamLyrics(t *testing.T) {
	live(t)
	songs, err := Search("不及我", 10)
	if err != nil {
		t.Fatal(err)
	}
	var free *Track
	for i := range songs {
		if !songs[i].VIP {
			free = &songs[i]
			break
		}
	}
	if free == nil {
		t.Skip("no free song found")
	}
	st, err := SongURL(*free, "high")
	if err != nil {
		t.Fatalf("SongURL: %v", err)
	}
	req, _ := http.NewRequest("GET", st.URL, nil)
	req.Header.Set("Range", "bytes=0-1023")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode/100 != 2 || len(b) != 1024 || st.Size == 0 {
		t.Errorf("stream %s: %s, %d bytes, size %d", st.URL, res.Status, len(b), st.Size)
	}
	lyric, _, err := Lyrics(Track{MID: "0039MnYb0qxYhV"})
	if err != nil || !strings.Contains(lyric, "LyricContent") {
		t.Errorf("lyrics: %.200q, %v", lyric, err)
	}
}

// The radar finds songs, signed out as in.
func TestLiveRadar(t *testing.T) {
	live(t)
	radar, _, err := RadarTracks(1)
	if err != nil || len(radar) == 0 {
		t.Errorf("radar: %d, %v", len(radar), err)
	}
}

// The QR codes to sign in with load.
func TestLiveQR(t *testing.T) {
	live(t)
	for _, kind := range []LoginKind{LoginQQMusic, LoginQQ, LoginWeChat} {
		q, err := StartQR(kind)
		if err != nil {
			t.Errorf("StartQR(%d): %v", kind, err)
			continue
		}
		if len(q.Image) < 100 {
			t.Errorf("StartQR(%d): image of %d bytes", kind, len(q.Image))
		}
		state, err := q.Poll()
		if err != nil || state != QRWaiting {
			t.Errorf("Poll(%d): %v, %v", kind, state, err)
		}
		q.Close()
	}
}
