package qqmusic

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"
)

// SearchType is what a search looks for, as the gateway numbers it.
type SearchType int

const (
	SearchSongs     SearchType = 0
	SearchArtists   SearchType = 1
	SearchAlbums    SearchType = 2
	SearchPlaylists SearchType = 3
)

// SearchResult is a page of what a search found, of the one type it
// looked for, and how many it found in all.
type SearchResult struct {
	Songs     []Track
	Albums    []AlbumProfile
	Artists   []ArtistProfile
	Playlists []Playlist
	Total     int
}

// Len is how many the page has.
func (r *SearchResult) Len() int {
	return len(r.Songs) + len(r.Albums) + len(r.Artists) + len(r.Playlists)
}

// Append adds the next page of a search to r.
func (r *SearchResult) Append(next SearchResult) {
	r.Songs = append(r.Songs, next.Songs...)
	r.Albums = append(r.Albums, next.Albums...)
	r.Artists = append(r.Artists, next.Artists...)
	r.Playlists = append(r.Playlists, next.Playlists...)
	r.Total = next.Total
}

// searchID is a search's ID, made as the client makes them.
func searchID() string {
	e := uint64(rand.IntN(20) + 1)
	n := uint64(rand.IntN(4194305))
	ms := uint64(time.Now().UnixMilli() % (24 * 60 * 60 * 1000))
	return fmt.Sprint(e*18014398509481984 + n*4294967296 + ms)
}

// SearchPage returns limit of what matches a query, of a type, from
// offset, which is a multiple of limit: the gateway pages by page.
func SearchPage(query string, kind SearchType, limit, offset int) (SearchResult, error) {
	limit = min(max(limit, 1), 50)
	data, err := call("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", obj{
		"grp": 0, "num_per_page": limit, "page_num": offset/limit + 1,
		"query": strings.TrimSpace(query), "search_type": int(kind), "searchid": searchID(),
	})
	if err != nil {
		return SearchResult{}, err
	}
	body := get(data, "body")
	r := SearchResult{Total: int(num(get(data, "meta"), "sum", "estimate_sum"))}
	switch kind {
	case SearchSongs:
		r.Songs = tracks(arr(get(body, "song"), "list"))
	case SearchArtists:
		for _, v := range arr(get(body, "singer"), "list") {
			ar := ArtistProfile{Artist: Artist{ID: num(v, "singerID"), MID: str(v, "singerMID"), Name: str(v, "singerName")}}
			ar.Avatar = https(str(v, "singerPic"))
			ar.MusicSize, ar.AlbumSize = int(num(v, "songNum")), int(num(v, "albumNum"))
			if ar.MID != "" {
				r.Artists = append(r.Artists, ar)
			}
		}
	case SearchAlbums:
		for _, v := range arr(get(body, "album"), "list") {
			al := AlbumProfile{Album: Album{ID: num(v, "albumID"), MID: str(v, "albumMID"), Name: str(v, "albumName")}}
			al.PicURL = albumCover(al.MID)
			al.PublishTime = dateMillis(str(v, "publicTime"))
			al.Size = int(num(v, "song_count"))
			for _, s := range arr(v, "singer_list") {
				al.Artists = append(al.Artists, Artist{ID: num(s, "id"), MID: str(s, "mid"), Name: str(s, "name")})
			}
			if len(al.Artists) == 0 {
				al.Artists = []Artist{{ID: num(v, "singerID"), MID: str(v, "singerMID"), Name: str(v, "singerName")}}
			}
			if al.MID != "" {
				r.Albums = append(r.Albums, al)
			}
		}
	case SearchPlaylists:
		for _, v := range arr(get(body, "songlist"), "list") {
			if pl, ok := playlistSummary(v); ok {
				r.Playlists = append(r.Playlists, pl)
			}
		}
	}
	return r, nil
}

// Search returns the songs matching a query.
func Search(query string, limit int) ([]Track, error) {
	res, err := SearchPage(query, SearchSongs, limit, 0)
	return res.Songs, err
}

// dateMillis reads a date, as 2005-11-01, in Unix milliseconds.
func dateMillis(s string) int64 {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), beijing)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// beijing is QQ Music's time.
var beijing = time.FixedZone("CST", 8*60*60)

// Suggestions are what a search may be after, as its query is typed, in
// the order QQ Music ranks their kinds.
type Suggestions struct {
	Songs   []Track
	Artists []Artist
	Albums  []AlbumProfile
	Order   []string // "songs", "artists" and "albums"
}

// SearchSuggest returns what a search for query may be after.
func SearchSuggest(query string) (Suggestions, error) {
	q := url.Values{"key": {query}, "format": {"json"}, "inCharset": {"utf-8"}, "outCharset": {"utf-8"}, "platform": {"yqq.json"}, "needNewCode": {"0"}}
	req, err := newGet("https://c.y.qq.com/splcloud/fcgi-bin/smartbox_new.fcg?" + q.Encode())
	if err != nil {
		return Suggestions{}, err
	}
	res, err := send(req)
	if err != nil {
		return Suggestions{}, err
	}
	d := get(res, "data")
	var s Suggestions
	kinds := []struct {
		key, name string
		order     int64
	}{{"song", "songs", 0}, {"singer", "artists", 1}, {"album", "albums", 2}}
	for i := range kinds {
		kinds[i].order = num(get(d, kinds[i].key), "order")
	}
	for range kinds {
		// The kinds by their order: few, so picked one at a time.
		best := -1
		for i, k := range kinds {
			if k.key != "" && (best < 0 || k.order < kinds[best].order) {
				best = i
			}
		}
		s.Order = append(s.Order, kinds[best].name)
		kinds[best].key = ""
	}
	for _, v := range arr(get(d, "song"), "itemlist") {
		t := Track{ID: num(v, "id"), MID: str(v, "mid"), Name: str(v, "name")}
		if singer := str(v, "singer"); singer != "" {
			t.Artists = []Artist{{Name: singer}}
		}
		s.Songs = append(s.Songs, t)
	}
	for _, v := range arr(get(d, "singer"), "itemlist") {
		s.Artists = append(s.Artists, Artist{ID: num(v, "id"), MID: str(v, "mid"), Name: str(v, "name")})
	}
	for _, v := range arr(get(d, "album"), "itemlist") {
		al := AlbumProfile{Album: Album{ID: num(v, "id"), MID: str(v, "mid"), Name: str(v, "name"), PicURL: https(str(v, "pic"))}}
		if singer := str(v, "singer"); singer != "" {
			al.Artists = []Artist{{Name: singer}}
		}
		s.Albums = append(s.Albums, al)
	}
	return s, nil
}

// AlbumProfile is an album as its page shows it.
type AlbumProfile struct {
	Album
	Artists     []Artist `json:"artists"`
	PublishTime int64    `json:"publishTime"` // Unix milliseconds
	Size        int      `json:"size"`        // how many songs it has
	Description string   `json:"description"`
	Kind        string   `json:"kind"` // 录音室专辑, Single, EP…
	Songs       []Track  `json:"-"`
}

// ArtistNames joins the album's artists' names, as its credits read.
func (al *AlbumProfile) ArtistNames() string { return artistNames(al.Artists) }

// AlbumDetail returns an album, by its MID, with its songs.
func AlbumDetail(mid string) (*AlbumProfile, error) {
	info, err := call("music.musichallAlbum.AlbumInfoServer", "GetAlbumDetail", obj{"albumMId": mid})
	if err != nil {
		return nil, err
	}
	b := get(info, "basicInfo")
	al := &AlbumProfile{Album: Album{ID: num(b, "albumID"), MID: mid, Name: str(b, "albumName")}}
	al.PicURL = albumCover(mid)
	al.PublishTime = dateMillis(str(b, "publishDate"))
	al.Description = str(b, "desc")
	al.Kind = str(b, "albumType")
	for _, s := range arr(get(info, "singer"), "singerList") {
		al.Artists = append(al.Artists, Artist{ID: num(s, "singerID"), MID: str(s, "mid"), Name: str(s, "name")})
	}
	for begin := 0; ; {
		data, err := call("music.musichallAlbum.AlbumSongList", "GetAlbumSongList", obj{"albumMid": mid, "begin": begin, "num": 100, "order": 2})
		if err != nil {
			return nil, err
		}
		list := arr(data, "songList")
		al.Songs = append(al.Songs, tracks(list)...)
		begin += len(list)
		if len(list) == 0 || begin >= int(num(data, "totalNum")) {
			break
		}
	}
	al.Size = len(al.Songs)
	return al, nil
}

// ArtistProfile is who an artist is, as their page shows them, with the
// songs they are best known for.
type ArtistProfile struct {
	Artist
	Avatar    string   `json:"avatar"` // square
	PicURL    string   `json:"picUrl"` // wide
	Alias     []string `json:"alias"`
	Trans     string   `json:"trans"`
	BriefDesc string   `json:"briefDesc"`
	MusicSize int      `json:"musicSize"`
	AlbumSize int      `json:"albumSize"`
	Followed  bool     `json:"followed"` // by the listener signed in
	Songs     []Track  `json:"-"`
}

// ArtistDetail returns an artist, by their MID, with their 50 most popular
// songs.
func ArtistDetail(mid string) (*ArtistProfile, error) {
	data, err := call("music.musichallSinger.SingerInfoInter", "GetSingerDetail", obj{
		"singer_mids": []string{mid}, "pic": 1, "group_singer": 1, "wiki_singer": 1, "ex_singer": 1,
	})
	if err != nil {
		return nil, err
	}
	s := get(data, "singer_list", "0")
	basic, ex := get(s, "basic_info"), get(s, "ex_info")
	ar := &ArtistProfile{Artist: Artist{ID: num(basic, "singer_id"), MID: mid, Name: str(basic, "name")}}
	ar.Avatar = ArtistPhoto(mid)
	ar.PicURL = https(str(get(s, "pic"), "big_black", "pic"))
	ar.Trans = str(ex, "foreign_name")
	ar.BriefDesc = str(ex, "desc")
	songs, total, err := ArtistSongs(mid, 50, 0, false)
	if err != nil {
		return nil, err
	}
	ar.Songs, ar.MusicSize = songs, total
	if albums, err := call("music.musichallAlbum.AlbumListServer", "GetAlbumList", obj{"singerMid": mid, "order": 0, "begin": 0, "num": 1, "songNumTag": 0, "singerID": 0}); err == nil {
		ar.AlbumSize = int(num(albums, "total"))
	}
	ar.Followed = followsArtist(mid)
	return ar, nil
}

// ArtistSongs returns limit of an artist's songs from offset, the newest
// first if latest, else the most popular, and how many they have.
func ArtistSongs(mid string, limit, offset int, latest bool) ([]Track, int, error) {
	order := 1
	if latest {
		order = 0
	}
	data, err := call("musichall.song_list_server", "GetSingerSongList", obj{"singerMid": mid, "order": order, "begin": offset, "num": limit})
	if err != nil {
		return nil, 0, err
	}
	return tracks(arr(data, "songList")), int(num(data, "totalNum")), nil
}

// ArtistLatestSongs returns up to limit of an artist's songs, the newest
// first.
func ArtistLatestSongs(mid string, limit int) ([]Track, error) {
	var out []Track
	for len(out) < limit {
		songs, total, err := ArtistSongs(mid, min(100, limit-len(out)), len(out), true)
		if err != nil {
			return out, err
		}
		out = append(out, songs...)
		if len(songs) == 0 || len(out) >= total {
			break
		}
	}
	return out, nil
}

// ArtistAlbums returns limit of an artist's albums from offset, the newest
// first, and whether they have more.
func ArtistAlbums(mid string, limit, offset int) ([]AlbumProfile, bool, error) {
	data, err := call("music.musichallAlbum.AlbumListServer", "GetAlbumList", obj{
		"singerMid": mid, "order": 0, "begin": offset, "num": limit, "songNumTag": 0, "singerID": 0,
	})
	if err != nil {
		return nil, false, err
	}
	var out []AlbumProfile
	for _, v := range arr(data, "albumList") {
		al := AlbumProfile{Album: Album{ID: num(v, "albumID"), MID: str(v, "albumMid"), Name: str(v, "albumName")}}
		al.PicURL = albumCover(al.MID)
		al.PublishTime = dateMillis(str(v, "publishDate"))
		al.Kind = str(v, "albumType")
		al.Artists = []Artist{{Name: str(v, "singerName")}}
		if al.MID != "" {
			out = append(out, al)
		}
	}
	return out, offset+len(out) < int(num(data, "total")), nil
}

// FollowedArtists returns the artists the listener follows.
func FollowedArtists() ([]ArtistProfile, error) {
	cred, err := fresh()
	if err != nil {
		return nil, err
	}
	euin := encryptedUIN(cred)
	var out []ArtistProfile
	for from := 0; ; {
		data, err := callAs(cred, "music.concern.RelationList", "GetFollowSingerList", obj{"HostUin": euin, "From": from, "Size": 100}, nil)
		if err != nil {
			return out, err
		}
		list := arr(data, "List")
		for _, v := range list {
			ar := ArtistProfile{Artist: Artist{MID: str(v, "MID", "Mid"), Name: str(v, "Name")}}
			ar.Avatar = https(str(v, "HeadUrl"))
			if ar.MID != "" {
				out = append(out, ar)
			}
		}
		from += len(list)
		if len(list) == 0 || from >= int(num(data, "Total")) {
			return out, nil
		}
	}
}

// followsArtist reports whether the listener follows the artist mid, as
// far as known.
func followsArtist(mid string) bool {
	if !SignedIn() {
		return false
	}
	followed, err := FollowedArtists()
	if err != nil {
		return false
	}
	for _, ar := range followed {
		if ar.MID == mid {
			return true
		}
	}
	return false
}

// FollowArtist follows an artist for the listener, or unfollows them,
// through the web page's CGI.
func FollowArtist(mid string, follow bool) error {
	cred, err := fresh()
	if err != nil {
		return err
	}
	op := "del"
	if follow {
		op = "add"
	}
	q := url.Values{
		"g_tk": {fmt.Sprint(hash33(cred.MusicKey))}, "loginUin": {fmt.Sprint(cred.MusicID)}, "hostUin": {"0"},
		"format": {"json"}, "inCharset": {"utf8"}, "outCharset": {"utf-8"}, "notice": {"0"}, "platform": {"yqq.json"},
		"needNewCode": {"0"}, "singermid": {mid}, "rnd": {fmt.Sprint(time.Now().UnixMilli())},
	}
	req, err := newGet("https://c.y.qq.com/rsc/fcgi-bin/fcg_order_singer_" + op + ".fcg?" + q.Encode())
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", cred.cookie())
	res, err := send(req)
	if err != nil {
		return err
	}
	if code := num(res, "code"); code != 0 {
		return &APIError{Method: "fcg_order_singer_" + op, Code: code}
	}
	return nil
}
