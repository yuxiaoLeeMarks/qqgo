package qqmusic

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"testing"
)

// A block decrypts as upstream's vector says.
func TestQRCBlock(t *testing.T) {
	b, _ := hex.DecodeString("0123456789abcdef")
	qrcDecrypt(b)
	if got := hex.EncodeToString(b); got != "5a92c2297192224e" {
		t.Errorf("decrypted %s, want 5a92c2297192224e", got)
	}
}

// Lyrics encrypted as QQ encrypts them come back as they were.
func TestDecryptQRC(t *testing.T) {
	const lyric = `<QrcInfos><LyricInfo><Lyric_1 LyricContent="[1000,500]原(1000,250)文(1250,250)"/></LyricInfo></QrcInfos>`
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte(lyric))
	w.Close()
	b := z.Bytes()
	b = append(b, make([]byte, (8-len(b)%8)%8)...)
	qrcEncrypt(b)
	got, err := decryptQRC(hex.EncodeToString(b))
	if err != nil || got != lyric {
		t.Errorf("decryptQRC = %q, %v", got, err)
	}
}
