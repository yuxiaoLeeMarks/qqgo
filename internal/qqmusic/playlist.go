package qqmusic

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Playlists and charts, the listener's own, and their liked songs, which
// QQ Music keeps as the playlist of directory 201.

const likedDir = 201

// The ID of the listener's liked songs' playlist, once their playlists
// are listed: its songs are asked for by its directory.
var (
	likedMu  sync.Mutex
	likedTID int64
)

// LikedPlaylist reports whether id is the listener's liked songs'.
func LikedPlaylist(id int64) bool {
	likedMu.Lock()
	defer likedMu.Unlock()
	return id != 0 && id == likedTID
}

// playlistSummary reads a playlist as lists of them give it.
func playlistSummary(v any) (Playlist, bool) {
	pl := Playlist{
		ID:   num(v, "tid", "dissid", "dissId", "id"),
		Name: str(v, "title", "dissname", "dissName", "dirName", "dirname", "name"),
	}
	if pl.ID == 0 || strings.TrimSpace(pl.Name) == "" {
		return pl, false
	}
	pl.DirID = num(v, "dirId", "dirid")
	pl.CoverURL = https(str(get(v, "cover"), "default_url", "medium_url", "big_url"))
	if pl.CoverURL == "" {
		pl.CoverURL = https(str(v, "imgurl", "imgUrl", "bigpicUrl", "picUrl", "picurl", "logo"))
	}
	pl.Description = str(v, "desc", "introduction", "description")
	pl.PlayCount = float(v, "play_cnt", "listennum", "listenNum", "playCnt")
	pl.TrackCount = int(num(v, "song_cnt", "song_count", "songNum", "songnum", "total_song_num"))
	creator := get(v, "creator")
	pl.Creator.Nickname = str(creator, "nick", "name", "nickname")
	if pl.Creator.Nickname == "" {
		pl.Creator.Nickname = str(v, "nick", "nickname", "creatorName", "host_nick")
	}
	pl.Creator.UserID = num(creator, "musicid", "uin", "qq", "creator_uin")
	return pl, true
}

// FeaturedPlaylists returns the playlists of the playlist square,
// recommended to the listener if signed in.
func FeaturedPlaylists(limit int) ([]Playlist, error) {
	data, err := call("music.playlist.PlaylistSquare", "GetRecommendFeed", obj{"From": 0, "Size": limit})
	if err != nil {
		return nil, err
	}
	var out []Playlist
	seen := map[int64]bool{}
	for _, e := range arr(data, "List") {
		pl, ok := playlistSummary(get(e, "Playlist", "basic"))
		if ok && !seen[pl.ID] {
			seen[pl.ID] = true
			out = append(out, pl)
		}
	}
	return out, nil
}

// chartPeriods says how often a chart changes, by the shape of its issue:
// a day's is 2026-10-30, a week's 2026_43.
func chartFreq(period string) string {
	switch {
	case strings.Contains(period, "-"):
		return "每日更新"
	case strings.Contains(period, "_"):
		return "每周更新"
	}
	return ""
}

func chart(v any) Playlist {
	top := num(v, "topId")
	pl := Playlist{ID: -top, Name: str(v, "title", "musichallTitle"), period: str(v, "period")}
	pl.CoverURL = https(str(v, "frontPicUrl", "headPicUrl", "mbFrontPicUrl"))
	pl.Description = strings.ReplaceAll(str(v, "intro"), "<br>", "\n")
	pl.PlayCount = float(v, "listenNum")
	pl.TrackCount = int(num(v, "totalNum"))
	pl.UpdateFreq = chartFreq(pl.period)
	pl.Creator.Nickname = "QQ 音乐"
	for _, s := range arr(v, "song")[:min(3, len(arr(v, "song")))] {
		pl.Preview = append(pl.Preview, ChartLine{First: str(s, "title"), Second: str(s, "singerName")})
	}
	return pl
}

// Charts returns the charts, each with its top three songs: the peak
// charts first, as QQ Music groups them.
func Charts() ([]Playlist, error) {
	data, err := call("music.musicToplist.Toplist", "GetAll", obj{})
	if err != nil {
		return nil, err
	}
	var out []Playlist
	for _, g := range arr(data, "group") {
		for _, v := range arr(g, "toplist") {
			if num(v, "topId") > 0 {
				out = append(out, chart(v))
			}
		}
	}
	return out, nil
}

// maxTracks is the most of a playlist's songs loaded.
const maxTracks = 1000

// PlaylistDetail returns a playlist, or a chart, with up to 1000 of its
// songs.
func PlaylistDetail(id int64) (*Playlist, error) {
	if IsChart(id) {
		return chartDetail(-id)
	}
	liked := LikedPlaylist(id)
	var cred *Credential
	if liked {
		var err error
		if cred, err = fresh(); err != nil {
			return nil, err
		}
	}
	var pl *Playlist
	for begin := 0; begin < maxTracks; {
		param := obj{"disstid": id, "dirid": 0, "tag": 1, "song_begin": begin, "song_num": 200,
			"userinfo": 1, "orderlist": 1, "onlysonglist": 0}
		if liked {
			param["disstid"], param["dirid"], param["enc_host_uin"] = 0, likedDir, encryptedUIN(cred)
		}
		data, err := call("music.srfDissInfo.DissInfo", "CgiGetDiss", param)
		if err != nil {
			return nil, err
		}
		if c := num(data, "code"); c != 0 {
			return nil, &APIError{Method: "CgiGetDiss", Code: c}
		}
		if pl == nil {
			info := get(data, "dirinfo")
			p, _ := playlistSummary(info)
			p.ID = id
			if p.Name == "" {
				p.Name = str(info, "title")
			}
			p.CoverURL = https(str(info, "picurl", "picurl2"))
			p.Creator.Nickname = str(get(info, "creator"), "nick")
			if p.Creator.Nickname == "" {
				p.Creator.Nickname = str(info, "host_nick")
			}
			p.Creator.UserID = num(get(info, "creator"), "musicid")
			p.TrackCount = int(num(data, "total_song_num"))
			if liked {
				p.SpecialType, p.DirID = 5, likedDir
				if p.Name == "" {
					p.Name = "我喜欢"
				}
			}
			pl = &p
		}
		list := arr(data, "songlist")
		pl.Tracks = append(pl.Tracks, tracks(list)...)
		begin += len(list)
		if len(list) == 0 || num(data, "hasmore") == 0 || begin >= pl.TrackCount {
			break
		}
	}
	return pl, nil
}

func chartDetail(top int64) (*Playlist, error) {
	var pl *Playlist
	for offset := 0; ; {
		param := obj{"topId": top, "offset": offset, "num": 100}
		if pl != nil {
			param["period"] = pl.period
		}
		data, err := call("music.musicToplist.Toplist", "GetDetail", param)
		if err != nil {
			return nil, err
		}
		if pl == nil {
			p := chart(get(data, "data"))
			p.Preview = nil
			pl = &p
		}
		list := arr(data, "songInfoList")
		pl.Tracks = append(pl.Tracks, tracks(list)...)
		offset += len(list)
		if len(list) == 0 || offset >= pl.TrackCount {
			return pl, nil
		}
	}
}

// UserPlaylists returns the playlists the listener made, their liked
// songs first, and those they saved.
func UserPlaylists() (made, saved []Playlist, err error) {
	cred, err := fresh()
	if err != nil {
		return nil, nil, err
	}
	data, err := callAs(cred, "music.musicasset.PlaylistBaseRead", "GetPlaylistByUin", obj{"uin": fmt.Sprint(cred.MusicID)}, nil)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range findArr(data, "v_playlist", "playlist") {
		pl, ok := playlistSummary(v)
		if !ok {
			continue
		}
		if pl.DirID == likedDir {
			pl.SpecialType = 5
			if pl.Name == "" || pl.Name == "我喜欢" {
				pl.Name = "我喜欢的音乐"
			}
			likedMu.Lock()
			likedTID = pl.ID
			likedMu.Unlock()
			made = append([]Playlist{pl}, made...)
			continue
		}
		made = append(made, pl)
	}
	euin := encryptedUIN(cred)
	for offset := 0; euin != ""; {
		data, err := callAs(cred, "music.musicasset.PlaylistFavRead", "CgiGetPlaylistFavInfo", obj{"uin": euin, "offset": offset, "size": 100}, nil)
		if err != nil {
			break
		}
		list := findArr(data, "v_list", "playlist")
		for _, v := range list {
			if pl, ok := playlistSummary(v); ok {
				saved = append(saved, pl)
			}
		}
		offset += len(list)
		if len(list) == 0 || !flag(data, "hasmore", "has_more") {
			break
		}
	}
	return made, saved, nil
}

// DailySongs returns the day's 30 songs picked for the listener, or,
// signed out, what many listen to: the top of the hot songs' chart.
func DailySongs() ([]Track, error) {
	if !SignedIn() {
		pl, err := chartDetail(26)
		if err != nil {
			return nil, err
		}
		return pl.Tracks[:min(30, len(pl.Tracks))], nil
	}
	data, err := callSignedIn("music.srfDissInfo.aiDissInfo", "uniform_get_Dissinfo", obj{"dirid": 202, "optype": 3, "tag": 1, "userinfo": 0, "pic_dpi": 300})
	if err != nil {
		return nil, err
	}
	if c := num(data, "code"); c != 0 {
		return nil, &APIError{Method: "uniform_get_Dissinfo", Code: c}
	}
	return tracks(arr(data, "songlist")), nil
}

// LikedIDs returns the songs the listener likes.
func LikedIDs() ([]int64, error) {
	cred, err := fresh()
	if err != nil {
		return nil, err
	}
	euin := encryptedUIN(cred)
	var ids []int64
	for begin := 0; begin < 5000; {
		data, err := callAs(cred, "music.srfDissInfo.DissInfo", "CgiGetDiss", obj{
			"disstid": 0, "dirid": likedDir, "enc_host_uin": euin, "tag": 0, "song_begin": begin, "song_num": 500,
			"userinfo": 0, "orderlist": 1, "onlysonglist": 1,
		}, nil)
		if err != nil {
			return ids, err
		}
		list := arr(data, "songlist")
		for _, t := range tracks(list) {
			ids = append(ids, t.ID)
		}
		begin += len(list)
		if len(list) == 0 || num(data, "hasmore") == 0 {
			break
		}
	}
	return ids, nil
}

// songInfo is how songs are named to the playlists they go in or out of.
func songInfo(tracks []Track) []obj {
	out := make([]obj, len(tracks))
	for i, t := range tracks {
		out[i] = obj{"songId": t.ID, "songType": t.Type}
	}
	return out
}

// ErrInPlaylist is what adding a song a playlist has already gives.
var ErrInPlaylist = errors.New("the song is in the playlist already")

// editSongs adds songs to the listener's playlist of directory dir, or
// takes them out.
func editSongs(dir int64, add bool, tracks []Track) error {
	method := "DelSonglist"
	if add {
		method = "AddSonglist"
	}
	data, err := callSignedIn("music.musicasset.PlaylistDetailWrite", method, obj{"dirId": dir, "bFmtUtf8": true, "v_songInfo": songInfo(tracks)})
	var api *APIError
	if errors.As(err, &api) && api.Code == 80092 {
		return ErrInPlaylist
	}
	if err != nil {
		return err
	}
	if c := num(data, "retCode", "ret_code"); c != 0 {
		return &APIError{Method: method, Code: c}
	}
	return nil
}

// Like likes a song, adding it to the listener's liked songs, or unlikes it.
func Like(t Track, like bool) error {
	err := editSongs(likedDir, like, []Track{t})
	if errors.Is(err, ErrInPlaylist) {
		return nil
	}
	return err
}

// AddToPlaylist adds songs to the listener's playlist of directory dir.
func AddToPlaylist(dir int64, songs ...Track) error { return editSongs(dir, true, songs) }

// RemoveFromPlaylist takes songs out of the listener's playlist of
// directory dir.
func RemoveFromPlaylist(dir int64, songs ...Track) error { return editSongs(dir, false, songs) }

// CreatePlaylist makes a playlist named name for the listener, and returns
// it, empty.
func CreatePlaylist(name string) (*Playlist, error) {
	data, err := callSignedIn("music.musicasset.PlaylistBaseWrite", "AddPlaylist", obj{"dirName": name})
	if err != nil {
		return nil, err
	}
	if c := num(data, "retCode"); c != 0 {
		return nil, &APIError{Method: "AddPlaylist", Code: c}
	}
	r := get(data, "result")
	pl := &Playlist{ID: num(r, "tid"), DirID: num(r, "dirId"), Name: str(r, "dirName")}
	if pl.Name == "" {
		pl.Name = name
	}
	if pl.ID == 0 || pl.DirID == 0 {
		return nil, errors.New("qqmusic: the new playlist came without its ID")
	}
	return pl, nil
}

// DeletePlaylist deletes the listener's playlist of directory dir, for good.
func DeletePlaylist(dir int64) error {
	data, err := callSignedIn("music.musicasset.PlaylistBaseWrite", "DelPlaylist", obj{"dirId": dir})
	if err != nil {
		return err
	}
	if c := num(data, "retCode"); c != 0 {
		return &APIError{Method: "DelPlaylist", Code: c}
	}
	return nil
}
