package app

import (
	"os"
	"path/filepath"
	"testing"

	"qqgo/internal/player"
	"qqgo/internal/qqmusic"
)

// TestMain keeps what the tests write, as the session kept, out of the
// listener's own directories.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "qqgo-test")
	if err == nil {
		os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
		os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

func newTestApp() *app {
	a := &app{
		playlists: map[int64]*qqmusic.Playlist{}, loadingPl: map[int64]bool{}, plErr: map[int64]string{},
		artists: map[string]*artistData{}, albums: map[string]*albumData{}, searches: map[route]*searchData{},
		lyricAt: -1, queueAt: -1,
	}
	a.art = newArtCache(func(func()) {})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}
