package app

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// searchRequest is a search asked of QQ Music.
type searchRequest struct {
	query  string
	kind   qqmusic.SearchType
	offset int
}

// fakeSearch replaces searching QQ Music with one that reports what it was
// asked, and finds nothing, which lands nowhere without a window.
func fakeSearch(t *testing.T) chan searchRequest {
	asked := make(chan searchRequest, 8)
	searchResults = func(q string, kind qqmusic.SearchType, _, offset int) (qqmusic.SearchResult, error) {
		asked <- searchRequest{q, kind, offset}
		return qqmusic.SearchResult{}, errors.New("offline")
	}
	t.Cleanup(func() { searchResults = qqmusic.SearchPage })
	return asked
}

func wait[T any](t *testing.T, ch chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatalf("%s wasn't asked for", what)
		panic("unreachable")
	}
}

// Enter searches songs for the query typed; a tab searches it for another
// kind, which the next search keeps, and back leaves the search.
func TestSearchKinds(t *testing.T) {
	asked := fakeSearch(t)
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type("春日影")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := wait(t, asked, "the search"); got != (searchRequest{"春日影", qqmusic.SearchSongs, 0}) {
		t.Fatalf("searched %+v", got)
	}

	if err := tt.Click("歌手"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := wait(t, asked, "the artists"); got != (searchRequest{"春日影", qqmusic.SearchArtists, 0}) {
		t.Fatalf("searched %+v", got)
	}
	a.search("迷星叫")
	if got := wait(t, asked, "the next search"); got.kind != qqmusic.SearchArtists {
		t.Errorf("the next search looked for kind %d", got.kind)
	}
	if !slices.Equal(a.settings.SearchHistory, []string{"迷星叫", "春日影"}) {
		t.Errorf("history %q", a.settings.SearchHistory)
	}

	tt.Key(0, ui.KeyBack)
	tt.Key(0, ui.KeyBack)
	if a.route.page != pageHome {
		t.Errorf("back went to %+v", a.route)
	}
}

// The tabs over a search's songs switch it to another kind: it crashed,
// the list's state reset as the list built the tabs in its header.
func TestSearchKindFromSongs(t *testing.T) {
	asked := fakeSearch(t)
	a := catalogApp()
	a.open(route{page: pageSearch, query: "MyGO", kind: qqmusic.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("歌手"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.kind != qqmusic.SearchArtists {
		t.Fatalf("on %+v", a.route)
	}
	if len(asked) > 0 {
		t.Error("the artists found were searched for again")
	}
}

// A search's next page loads once its foot comes into view, the page after
// those loaded, as QQ Music pages, and a search done again loads from its
// start.
func TestSearchLoadsMore(t *testing.T) {
	asked := fakeSearch(t)
	a := newTestApp()
	a.settings = defaultSettings()
	r := route{page: pageSearch, query: "春日影", kind: qqmusic.SearchPlaylists}
	a.searches[r] = &searchData{
		res:   qqmusic.SearchResult{Playlists: []qqmusic.Playlist{{ID: 1, Name: "甲"}, {ID: 2, Name: "乙"}}, Total: 500},
		pages: 1,
		page:  paged{loaded: true, more: true},
	}
	a.open(r)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if got := wait(t, asked, "the next page"); got != (searchRequest{"春日影", qqmusic.SearchPlaylists, searchLimit}) {
		t.Fatalf("asked for %+v", got)
	}

	a.search("春日影")
	if got := wait(t, asked, "the search again"); got.offset != 0 {
		t.Errorf("searched again from %d", got.offset)
	}
}

// A search before, from the empty field's popup, searches again, clicked
// or chosen with the arrows; it was lost when its option was keyed anew.
func TestSearchHistoryChoice(t *testing.T) {
	for _, keys := range []bool{false, true} {
		asked := fakeSearch(t)
		a := newTestApp()
		a.settings = defaultSettings()
		a.settings.SearchHistory = []string{"春日影", "迷星叫"}
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		if err := tt.Click("搜索"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if keys {
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyEnter)
		} else if err := tt.Click("迷星叫"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if got := wait(t, asked, "the search"); got.query != "迷星叫" || a.query != "迷星叫" {
			t.Fatalf("keys %v: searched %+v, field %q", keys, got, a.query)
		}
		if !slices.Equal(a.settings.SearchHistory, []string{"迷星叫", "春日影"}) {
			t.Errorf("keys %v: history %q", keys, a.settings.SearchHistory)
		}
	}
}

// Typing asks for suggestions once it pauses, and a suggested album opens
// its page, remembering the query.
func TestSuggestionOpensPage(t *testing.T) {
	asked := make(chan string, 4)
	searchSuggest = func(q string) (qqmusic.Suggestions, error) { asked <- q; return qqmusic.Suggestions{}, nil }
	defer func() { searchSuggest = qqmusic.SearchSuggest }()
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Click("搜索")
	tt.Type("迷跡")
	tt.Frame()
	if q := wait(t, asked, "suggestions"); q != "迷跡" {
		t.Fatalf("suggestions for %q", q)
	}
	// As they land, which post drops without a window.
	a.suggestions.loading = false
	a.suggestions.items = suggestionList(qqmusic.Suggestions{
		Albums: []qqmusic.AlbumProfile{{Album: qqmusic.Album{MID: "al8", Name: "迷跡波"}, Artists: []qqmusic.Artist{{Name: "MyGO!!!!!"}}}},
		Order:  []string{"albums"},
	})
	tt.Frame()
	if err := tt.Click("专辑：迷跡波"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pageAlbum, mid: "al8"}) || !slices.Equal(a.settings.SearchHistory, []string{"迷跡"}) {
		t.Fatalf("opened %+v, history %q", a.route, a.settings.SearchHistory)
	}
}

// An artist's 专辑 loads their first page of albums as it is chosen, and
// once; an album opens its page.
func TestArtistAlbums(t *testing.T) {
	albums := make(chan int, 4)
	albumsBy = func(mid string, _, offset int) ([]qqmusic.AlbumProfile, bool, error) {
		albums <- offset
		return nil, false, errors.New("offline")
	}
	albumDetail = func(string) (*qqmusic.AlbumProfile, error) { return nil, errors.New("offline") }
	defer func() { albumsBy, albumDetail = qqmusic.ArtistAlbums, qqmusic.AlbumDetail }()
	a := newTestApp()
	a.settings = defaultSettings()
	d := &artistData{profile: &qqmusic.ArtistProfile{Artist: qqmusic.Artist{MID: "a12", Name: "乙"}}}
	a.artists["a12"] = d
	a.open(route{page: pageArtist, mid: "a12"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if offset := wait(t, albums, "the albums"); offset != 0 {
		t.Fatalf("albums from %d", offset)
	}
	d.albums = []qqmusic.AlbumProfile{{Album: qqmusic.Album{MID: "al8", Name: "迷跡波"}}}
	d.albumsPage = paged{loaded: true}
	tt.Frame()
	if err := tt.Click("迷跡波"); err != nil {
		t.Fatal(err)
	}
	if a.route != (route{page: pageAlbum, mid: "al8"}) {
		t.Fatalf("the album opened %+v", a.route)
	}

	tt.Key(0, ui.KeyBack)
	tt.Frame()
	tt.Click("热门")
	tt.Frame()
	tt.Click("专辑")
	tt.Frame()
	if len(albums) > 0 {
		t.Error("the albums were asked for again")
	}
}

// 关注 follows an artist at once, and asks QQ Music to; a second click
// waits for the first.
func TestFollowArtist(t *testing.T) {
	asked := make(chan bool, 4)
	followArtist = func(mid string, follow bool) error { asked <- follow; return nil }
	defer func() { followArtist = qqmusic.FollowArtist }()
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &qqmusic.Profile{UserID: 1}
	a.artists["a12"] = &artistData{profile: &qqmusic.ArtistProfile{Artist: qqmusic.Artist{MID: "a12", Name: "乙"}}}
	a.open(route{page: pageArtist, mid: "a12"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("关注"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.artists["a12"].profile.Followed || !wait(t, asked, "the follow") {
		t.Fatal("not followed")
	}
	tt.Click("已关注")
	if len(asked) > 0 || !a.artists["a12"].profile.Followed {
		t.Error("unfollowed before the follow landed")
	}
}

// A song row's album opens its page, not playing the song as the row's
// click would.
func TestAlbumLinkInRow(t *testing.T) {
	albumDetail = func(string) (*qqmusic.AlbumProfile, error) { return nil, errors.New("offline") }
	defer func() { albumDetail = qqmusic.AlbumDetail }()
	a := newTestApp()
	a.settings = defaultSettings()
	song := qqmusic.Track{ID: 2, Name: "起风了", Artists: []qqmusic.Artist{{MID: "a13", Name: "丙"}}, Album: qqmusic.Album{MID: "al8", Name: "迷跡波"}, Duration: 200000}
	a.artists["a13"] = &artistData{profile: &qqmusic.ArtistProfile{Artist: qqmusic.Artist{MID: "a13", Name: "丙"}, Songs: []qqmusic.Track{song}}}
	a.open(route{page: pageArtist, mid: "a13"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Its name, at the start of the column it fills.
	r, ok := tt.Find("迷跡波")
	if !ok {
		t.Fatal("no album")
	}
	tt.ClickAt(r.X+8, r.Y+r.H/2)
	tt.Frame()
	if a.route != (route{page: pageAlbum, mid: "al8"}) || a.pl.Current() != nil {
		t.Fatalf("opened %+v, playing %+v", a.route, a.pl.Current())
	}
}

// catalogApp is an app with an artist and an album loaded, and searches
// for each kind, to look at.
func catalogApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	day := time.Date(2026, 10, 1, 20, 0, 0, 0, time.Local).UnixMilli()
	band := qqmusic.Artist{MID: "a7", Name: "MyGO!!!!!"}
	al := qqmusic.Album{MID: "al8", Name: "迷跡波"}
	var songs []qqmusic.Track
	for i, name := range []string{"春日影", "迷星叫", "壱雫空", "碧天伴走", "音一会", "栞"} {
		songs = append(songs, qqmusic.Track{ID: int64(i + 1), MID: fmt.Sprint("s", i), Name: name, Artists: []qqmusic.Artist{band}, Album: al, Duration: int64(210000 + 16000*i)})
	}
	var albums []qqmusic.AlbumProfile
	for i, name := range []string{"迷跡波", "跡暖空", "壱雫空", "音一会", "迷星叫", "焚音打"} {
		albums = append(albums, qqmusic.AlbumProfile{Album: qqmusic.Album{MID: fmt.Sprint("al", 20+i), Name: name}, Artists: []qqmusic.Artist{band}, PublishTime: day})
	}
	a.artists["a7"] = &artistData{
		profile: &qqmusic.ArtistProfile{Artist: band, Alias: []string{"マイゴ"}, MusicSize: 38, AlbumSize: 9, BriefDesc: "迷いながらでも、前へ。", Songs: songs},
		albums:  albums, albumsPage: paged{loaded: true},
	}
	a.albums["al8"] = &albumData{album: &qqmusic.AlbumProfile{Album: al, Artists: []qqmusic.Artist{band}, PublishTime: day, Kind: "录音室专辑", Description: "MyGO!!!!! 的第一张专辑。", Songs: songs}}
	playlists := []qqmusic.Playlist{{ID: 30, Name: "通勤路上", TrackCount: 40, PlayCount: 12000}, {ID: 31, Name: "睡前", TrackCount: 12}}
	for _, res := range []struct {
		kind qqmusic.SearchType
		res  qqmusic.SearchResult
	}{
		{qqmusic.SearchSongs, qqmusic.SearchResult{Songs: songs, Total: 38}},
		{qqmusic.SearchArtists, qqmusic.SearchResult{Artists: []qqmusic.ArtistProfile{*a.artists["a7"].profile, {Artist: qqmusic.Artist{MID: "a10", Name: "Ave Mujica"}}}, Total: 2}},
		{qqmusic.SearchAlbums, qqmusic.SearchResult{Albums: albums, Total: 6}},
		{qqmusic.SearchPlaylists, qqmusic.SearchResult{Playlists: playlists, Total: 2}},
	} {
		a.searches[route{page: pageSearch, query: "MyGO", kind: res.kind}] = &searchData{res: res.res, pages: 1, page: paged{loaded: true}}
	}
	return a
}

// catalogScenes are the pages of catalogApp.
var catalogScenes = []struct {
	name string
	r    route
	tab  int
}{
	{"search-songs", route{page: pageSearch, query: "MyGO", kind: qqmusic.SearchSongs}, 0},
	{"search-artists", route{page: pageSearch, query: "MyGO", kind: qqmusic.SearchArtists}, 0},
	{"search-albums", route{page: pageSearch, query: "MyGO", kind: qqmusic.SearchAlbums}, 0},
	{"search-playlists", route{page: pageSearch, query: "MyGO", kind: qqmusic.SearchPlaylists}, 0},
	{"artist", route{page: pageArtist, mid: "a7"}, artistHot},
	{"artist-albums", route{page: pageArtist, mid: "a7"}, artistAlbums},
	{"album", route{page: pageAlbum, mid: "al8"}, 0},
}

// With NM_FRAMES set, saves the pages of searches, artists and albums in
// the light and the dark.
func TestCatalogFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, s := range catalogScenes {
		for _, dark := range []bool{false, true} {
			a := catalogApp()
			a.artistTab = s.tab
			a.open(s.r)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Advance(time.Second)
			tt.Frame()
			name := s.name
			if dark {
				name += "-dark"
			}
			f, _ := os.Create(filepath.Join(dir, "catalog-"+name+".png"))
			png.Encode(f, tt.Image())
			f.Close()
		}
	}
}

// The pages of searches, artists and albums ask for no frames once shown.
func TestCatalogFrameBudget(t *testing.T) {
	for _, s := range catalogScenes {
		a := catalogApp()
		a.artistTab = s.tab
		a.open(s.r)
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Advance(time.Second)
		tt.Frame()
		tt.Advance(time.Second)
		if tt.FramePending() {
			t.Errorf("%s asks for frames while idle", s.name)
		}
	}
}
