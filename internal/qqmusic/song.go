package qqmusic

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("this song isn't available to stream")

// Stream is where a track streams from, and what it is.
type Stream struct {
	URL string `json:"-"`
	// Mirrors are the same file on other servers of the CDN, to try in
	// turn should URL fail.
	Mirrors []string `json:"-"`
	Size    int64    `json:"size"` // in bytes, or 0 if unknown
	// Type is the format: mp3, or flac for the lossless levels.
	Type string `json:"type"`
	// Level is the level served, which may be below the one asked for.
	Level string `json:"level"`
	// Gain and Peak are as the track's.
	Gain float64 `json:"gain"`
	Peak float64 `json:"peak"`
}

// Levels are the stream levels, from the lowest.
var Levels = []string{"standard", "high", "lossless", "hires"}

// Lossless reports whether level streams FLAC.
func Lossless(level string) bool { return level == "lossless" || level == "hires" }

// levelFile is how the files of a level are named: a prefix before the
// song's media MID, and their extension.
var levelFile = map[string][2]string{
	"standard": {"M500", ".mp3"},
	"high":     {"M800", ".mp3"},
	"lossless": {"F000", ".flac"},
	"hires":    {"RS01", ".flac"},
}

func fileName(t Track, level string) string {
	f := levelFile[level]
	if t.MediaMID != "" {
		return f[0] + t.MediaMID + f[1]
	}
	return f[0] + t.MID + t.MID + f[1]
}

// SongURL returns where to stream a track, MP3 or FLAC, at level (one of
// Levels) or the best below it the listener may play.
func SongURL(t Track, level string) (*Stream, error) {
	at := slices.Index(Levels, level)
	if at < 0 {
		at = 1
	}
	// The levels to ask for, the best first: those the song has files of.
	var levels, names, mids []string
	var types []int64
	for i := at; i >= 0; i-- {
		lv := Levels[i]
		if n, known := t.Sizes[lv]; known && n == 0 {
			continue
		}
		levels = append(levels, lv)
		names = append(names, fileName(t, lv))
		mids = append(mids, t.MID)
		types = append(types, t.Type)
	}
	if len(levels) == 0 {
		return nil, ErrUnavailable
	}
	cred := current()
	uin := "0"
	if cred != nil {
		uin = fmt.Sprint(cred.MusicID)
	}
	guid := device()
	if cred != nil {
		guid = cred.guid()
	}
	data, err := call("music.vkey.GetVkey", "UrlGetVkey", obj{
		"filename": names, "guid": guid, "songmid": mids, "songtype": types, "uin": uin, "ctx": 0,
	})
	if err != nil {
		return nil, err
	}
	// The best level whose file came with a key.
	paths := map[string]string{}
	for _, e := range arr(data, "midurlinfo") {
		purl := str(e, "purl")
		if purl == "" || num(e, "result") != 0 {
			continue
		}
		name := purl
		if i := strings.IndexByte(name, '?'); i >= 0 {
			name = name[:i]
		}
		for _, lv := range levels {
			f := levelFile[lv]
			if strings.HasPrefix(name, f[0]) && strings.HasSuffix(name, f[1]) {
				paths[lv] = purl
			}
		}
	}
	for _, lv := range levels {
		purl, ok := paths[lv]
		if !ok {
			continue
		}
		urls := streamURLs(purl, arr(data, "sip"))
		s := &Stream{URL: urls[0], Mirrors: urls[1:], Size: t.Sizes[lv], Type: "mp3", Level: lv, Gain: t.Gain, Peak: t.Peak}
		if Lossless(lv) {
			s.Type = "flac"
		}
		return s, nil
	}
	return nil, ErrUnavailable
}

// The servers songs stream from, as the CDN's dispatch says, kept for as
// long as it says.
var (
	cdnMu      sync.Mutex
	cdnServers []string
	cdnUntil   time.Time
)

// defaultServer is where songs stream from when the dispatch says nothing.
const defaultServer = "https://isure.stream.qqmusic.qq.com/"

func servers() []string {
	cdnMu.Lock()
	defer cdnMu.Unlock()
	if time.Now().Before(cdnUntil) {
		return cdnServers
	}
	cdnUntil = time.Now().Add(time.Minute) // a failed dispatch is asked again in a while
	data, err := callAs(nil, "music.audioCdnDispatch.cdnDispatch", "GetCdnDispatch", obj{
		"guid": device(), "uid": "0", "use_new_domain": 1, "use_ipv6": 1,
	}, nil)
	if err != nil {
		return cdnServers
	}
	cdnServers = directServers(arr(data, "sip"))
	if ttl := num(data, "cacheTime", "expiration"); ttl > 60 {
		cdnUntil = time.Now().Add(time.Duration(ttl) * time.Second)
	}
	return cdnServers
}

// directServers keeps the servers of list that serve songs directly: the
// "ws" ones are the last resort, as lyrune found.
func directServers(list []any) []string {
	var direct, ws []string
	for _, v := range list {
		s := asString(v)
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			continue
		}
		s = strings.TrimSuffix(s, "/") + "/"
		host := u.Hostname()
		label, _, _ := strings.Cut(host, ".")
		if label == "ws" || strings.HasPrefix(label, "ws") && strings.Trim(label[2:], "0123456789") == "" {
			ws = append(ws, s)
		} else if !slices.Contains(direct, s) {
			direct = append(direct, s)
		}
	}
	if len(direct) == 0 {
		return ws
	}
	return direct
}

// streamURLs returns where the file purl is, on each server to try: those
// the answer gave, then the CDN's, then the default.
func streamURLs(purl string, sip []any) []string {
	path := strings.TrimPrefix(purl, "/")
	if u, err := url.Parse(purl); err == nil && u.Host != "" {
		path = strings.TrimPrefix(u.RequestURI(), "/")
	}
	var out []string
	add := func(server string) {
		u := https(strings.TrimSuffix(server, "/") + "/" + path)
		if !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	for _, s := range directServers(sip) {
		add(s)
	}
	add(defaultServer)
	for _, s := range servers() {
		add(s)
	}
	return out
}

// Lyrics returns a track's lyrics: QQ's word-timed lyrics (QRC) where it
// has them, else timed lines (LRC), and their translation, if any.
func Lyrics(t Track) (lyric, translation string, err error) {
	data, err := call("music.musichallSong.PlayLyricInfo", "GetPlayLyricInfo", obj{
		"crypt": 1, "lrc_t": 0, "qrc": 1, "qrc_t": 0, "roma": 0, "roma_t": 0,
		"songMid": t.MID, "trans": 1, "trans_t": 0, "type": 1,
	})
	if err == nil {
		lyric, translation, err = readLyrics(data)
		if err == nil && strings.TrimSpace(lyric) != "" {
			return lyric, translation, nil
		}
	}
	data, err = call("music.musichallSong.PlayLyricInfo", "GetPlayLyricInfo", obj{
		"crypt": 0, "roma": 0, "songMID": t.MID, "trans": 1, "type": 0,
	})
	if err != nil {
		return "", "", err
	}
	return readLyrics(data)
}

func readLyrics(data any) (lyric, translation string, err error) {
	encrypted := num(data, "crypt") == 1
	text := func(key string) (string, error) {
		s := str(data, key)
		if strings.TrimSpace(s) == "" {
			return "", nil
		}
		if encrypted {
			return decryptQRC(s)
		}
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return string(b), nil
		}
		return s, nil
	}
	if lyric, err = text("lyric"); err != nil {
		return "", "", err
	}
	translation, _ = text("trans")
	return lyric, translation, nil
}

// decryptQRC decrypts lyrics as QQ encrypts them: hexadecimal, of triple
// DES of its own (see qrc.go), over zlib.
func decryptQRC(s string) (string, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("qrc: %w", err)
	}
	if len(b)%8 != 0 {
		return "", errors.New("qrc: not whole blocks")
	}
	qrcDecrypt(b)
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("qrc: %w", err)
	}
	out, err := io.ReadAll(r)
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("qrc: %w", err)
	}
	return string(out), nil
}

// URLs returns where the stream is: URL, then its mirrors.
func (s *Stream) URLs() []string { return append([]string{s.URL}, s.Mirrors...) }
