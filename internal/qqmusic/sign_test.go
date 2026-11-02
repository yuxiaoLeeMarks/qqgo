package qqmusic

import "testing"

// The hashes match lyrune's, which match the client's.
func TestHash33(t *testing.T) {
	for s, want := range map[string]int64{"": 5381, "a": 177670, "abc": 193485963, "腾讯": 6989618, "hello": 261238937} {
		if got := hash33(s); got != want {
			t.Errorf("hash33(%q) = %d, want %d", s, got, want)
		}
	}
}

// A body signs as lyrune signs it, which the gateway accepts.
func TestSign(t *testing.T) {
	if got, want := sign([]byte(`{"foo":"bar","num":1}`)), "zzcf3ea51dcp3xdwnxisjgufsk0znclehf2t85bc1d3d4"; got != want {
		t.Errorf("sign = %s, want %s", got, want)
	}
}
