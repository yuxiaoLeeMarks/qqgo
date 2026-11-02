package qqmusic

import (
	"fmt"
)

// QQ Music's endless recommendations: 猜你喜欢, its personal radio, a few
// songs at a time, and 专属雷达, songs found from what the listener saved,
// by the page.

// GuessTracks returns the next few songs of the listener's personal radio.
func GuessTracks() ([]Track, error) {
	data, err := callSignedIn("music.radioProxy.MbTrackRadioSvr", "get_radio_track", obj{
		"id": 99, "num": 10, "from": 0, "scene": 0, "song_ids": []int{},
	})
	if err != nil {
		return nil, err
	}
	return tracks(findArr(data, "Tracks", "tracks")), nil
}

// RadarTracks returns a page of the songs the radar found for the
// listener, from 1, and whether it has more.
func RadarTracks(page int) ([]Track, bool, error) {
	data, err := call("music.recommend.TrackRelationServer", "GetRadarSong", obj{
		"Page": max(page, 1), "ReqType": 0, "FavSongs": []int{}, "EntranceSongs": []int{},
	})
	if err != nil {
		return nil, false, err
	}
	songs := tracks(findArr(data, "VecSongs", "vecSongs"))
	more := len(songs) > 0
	if _, ok := field(data, "HasMore", "hasMore"); ok {
		more = more && flag(data, "HasMore", "hasMore")
	}
	return songs, more, nil
}

// Dislike tells QQ Music the listener does not like a song, which its
// recommendations leave out.
func Dislike(t Track) error {
	data, err := callSignedIn("music.feedback.FeedbackBlack", "AddDislike", obj{
		"Songs": []obj{{"ID": fmt.Sprint(t.ID), "IdType": 1}},
	})
	if err != nil {
		return err
	}
	if c := num(data, "Retcode", "retcode"); c != 0 {
		return &APIError{Method: "AddDislike", Code: c}
	}
	return nil
}
