package qqmusic

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// QQ Music's catalog, as its desktop client asks for it: what anyone may
// listen to, and once signed in, what is the listener's.

// Artist is a singer, or a band. QQ Music addresses them by MID.
type Artist struct {
	ID   int64  `json:"id"`
	MID  string `json:"mid"`
	Name string `json:"name"`
}

// Album is an album, addressed by MID; PicURL is its cover.
type Album struct {
	ID     int64  `json:"id"`
	MID    string `json:"mid"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type Track struct {
	ID  int64  `json:"id"`
	MID string `json:"mid"`
	// MediaMID names the song's files, which may differ from its MID.
	MediaMID string   `json:"mediaMid,omitempty"`
	Type     int64    `json:"type,omitempty"` // the kind of song, which its stream is asked for by
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	Alias    []string `json:"alia,omitempty"`
	// Reason says why a recommendation was picked.
	Reason string `json:"reason,omitempty"`
	// VIP says only a subscriber may play it whole.
	VIP bool `json:"vip,omitempty"`
	// Sizes are the bytes of the song's files, by level, as far as known:
	// 0 says it has none at that level.
	Sizes map[string]int64 `json:"sizes,omitempty"`
	// Gain is how many dB to change the song by to sound as loud as
	// others, and Peak its loudest sample, 1 at full scale: 0 if unknown.
	Gain float64 `json:"gain,omitempty"`
	Peak float64 `json:"peak,omitempty"`
}

// ArtistNames joins the track's artists as the credits read.
func (t *Track) ArtistNames() string { return artistNames(t.Artists) }

func artistNames(artists []Artist) string {
	names := make([]string, len(artists))
	for i, a := range artists {
		names[i] = a.Name
	}
	return strings.Join(names, " / ")
}

// Available reports whether QQ Music has the song to play at all: a song
// taken down has no files.
func (t *Track) Available() bool {
	if len(t.Sizes) == 0 {
		return true
	}
	for _, n := range t.Sizes {
		if n > 0 {
			return true
		}
	}
	return false
}

// Playable reports whether the listener may play the whole song: one only
// subscribers play takes signing in, and then the stream says.
func (t *Track) Playable() bool {
	return t.Available() && (!t.VIP || SignedIn())
}

func (t *Track) Length() time.Duration { return time.Duration(t.Duration) * time.Millisecond }

// URL is the song's page on the QQ Music website.
func (t *Track) URL() string { return "https://y.qq.com/n/ryqq/songDetail/" + t.MID }

// Playlist is a playlist, or a chart, whose ID is its top ID negated.
type Playlist struct {
	ID int64 `json:"id"`
	// DirID is the directory of one of the listener's own playlists, which
	// edits to it go by.
	DirID       int64   `json:"dirId,omitempty"`
	Name        string  `json:"name"`
	CoverURL    string  `json:"coverImgUrl"`
	Description string  `json:"description"`
	PlayCount   float64 `json:"playCount"`
	TrackCount  int     `json:"trackCount"`
	// UpdateFreq says how often a chart changes, which a playlist doesn't.
	UpdateFreq  string `json:"updateFrequency"`
	SpecialType int    `json:"specialType"` // 5: the listener's liked songs
	Creator     struct {
		UserID   int64  `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks []Track `json:"tracks"`
	// Preview holds a chart's top songs, as its card shows them.
	Preview []ChartLine `json:"-"`
	period  string      // the chart's issue
}

// ChartLine is a song at the top of a chart: its name and its artist.
type ChartLine struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

func (p *Playlist) Cover() string { return p.CoverURL }

// IsChart reports whether a playlist ID is a chart's.
func IsChart(id int64) bool { return id < 0 }

// albumCover is where the cover of an album is, by its MID.
func albumCover(mid string) string {
	if mid == "" {
		return ""
	}
	return "https://y.gtimg.cn/music/photo_new/T002R300x300M000" + mid + ".jpg"
}

// ArtistPhoto is where an artist's photo is, by their MID.
func ArtistPhoto(mid string) string {
	if mid == "" {
		return ""
	}
	return "https://y.gtimg.cn/music/photo_new/T001R300x300M000" + mid + ".jpg"
}

// stableID gives what has no ID one from its MID, below 0, to tell it from
// the others in the app's lists.
func stableID(mid string) int64 {
	h := fnv.New64a()
	h.Write([]byte(mid))
	return -int64(h.Sum64() >> 2)
}

// track reads a song in any of the shapes the endpoints give: wrapped as
// songInfo, track or Track, or not.
func track(v any) (Track, bool) {
	for _, k := range []string{"songInfo", "track", "Track"} {
		if w := get(v, k); isObject(w) {
			v = w
			break
		}
	}
	t := Track{
		ID:   num(v, "id", "songid", "songId"),
		MID:  str(v, "mid", "songmid", "songMid"),
		Type: num(v, "type", "songtype", "songType"),
		Name: str(v, "title", "name", "songname", "songName"),
	}
	if t.MID == "" || t.Name == "" {
		return t, false
	}
	if t.ID == 0 {
		t.ID = stableID(t.MID)
	}
	if sub := strings.TrimSpace(str(v, "subtitle")); sub != "" && !strings.Contains(t.Name, sub) {
		t.Alias = []string{sub}
	}
	t.Duration = num(v, "interval") * 1000
	for _, s := range arr(v, "singer", "singers") {
		ar := Artist{ID: num(s, "id", "singerID", "singerId"), MID: str(s, "mid", "singerMID", "singerMid", "singer_mid"), Name: str(s, "name", "title", "singerName")}
		if ar.Name != "" {
			t.Artists = append(t.Artists, ar)
		}
	}
	al := get(v, "album")
	t.Album = Album{ID: num(al, "id"), MID: str(al, "mid", "albumMid", "albummid"), Name: str(al, "name", "title")}
	if t.Album.MID == "" {
		t.Album.MID = str(v, "albumMid", "albummid")
	}
	// The cover is the album's picture, by its own MID where it has one:
	// an album QQ Music lists without a MID, as a live recording's, still
	// has its picture. Without either, an artist's photo stands in.
	pic := str(al, "pmid")
	if pic == "" {
		pic = t.Album.MID
	}
	t.Album.PicURL = albumCover(pic)
	for _, ar := range t.Artists {
		if t.Album.PicURL != "" {
			break
		}
		t.Album.PicURL = ArtistPhoto(ar.MID)
	}
	if f := get(v, "file"); isObject(f) {
		t.MediaMID = str(f, "media_mid", "mediaMid")
		t.Sizes = map[string]int64{}
		for level, keys := range map[string][]string{
			"standard": {"size_128mp3", "size128"},
			"high":     {"size_320mp3", "size320"},
			"lossless": {"size_flac", "sizeflac"},
			"hires":    {"size_hires", "sizeHires"},
		} {
			if _, ok := field(f, keys...); ok {
				t.Sizes[level] = num(f, keys...)
			}
		}
	}
	t.VIP = num(get(v, "pay"), "pay_play", "payplay") == 1
	if vol := get(v, "volume"); isObject(vol) {
		t.Gain, t.Peak = float(vol, "gain"), float(vol, "peak")
	}
	return t, true
}

// tracks reads the songs of vs, leaving out what is no song.
func tracks(vs []any) []Track {
	out := make([]Track, 0, len(vs))
	for _, v := range vs {
		if t, ok := track(v); ok {
			out = append(out, t)
		}
	}
	return out
}

// Sized asks the image CDN for a picture of about n×n pixels, of the sizes
// it has.
func Sized(pic string, n int) string {
	if pic == "" {
		return ""
	}
	pic = https(pic)
	switch {
	case photoSize.MatchString(pic):
		s := 150
		for _, size := range []int{150, 300, 500} {
			s = size
			if size >= n {
				break
			}
		}
		return photoSize.ReplaceAllString(pic, fmt.Sprintf("R%dx%d", s, s))
	case strings.Contains(pic, "qpic.y.qq.com/music_cover/"):
		// A playlist's cover: its size is the last of its path.
		base, query, _ := strings.Cut(pic, "?")
		if i := strings.LastIndex(base, "/"); i > 0 {
			s := "300"
			if n > 300 {
				s = "600"
			}
			base = base[:i+1] + s
		}
		if query != "" {
			return base + "?" + query
		}
		return base
	}
	return pic
}

var photoSize = regexp.MustCompile(`R\d+x\d+`)

// Fetch returns what is at url, as a cover the image CDN serves.
func Fetch(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	return io.ReadAll(res.Body)
}
