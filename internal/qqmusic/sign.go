package qqmusic

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// hash33 is the hash QQ's login keys go through to make g_tk, which
// requests carry beside the cookie.
func hash33(s string) int64 {
	h := uint64(5381)
	for _, r := range s {
		h = h*33 + uint64(r)
	}
	return int64(h & 0x7fffffff)
}

// The sign of a gateway request, as the desktop client makes it from the
// request's body: adapted from lyrune, after netease-qq-music-api (MIT).
var (
	signPart1    = []int{23, 14, 6, 36, 16, 40, 7, 19}
	signPart2    = []int{16, 1, 32, 12, 19, 27, 8, 5}
	signScramble = []byte{89, 39, 179, 150, 218, 82, 58, 252, 177, 52, 186, 123, 120, 64, 242, 133, 143, 161, 121, 179}
)

// sign returns the sign of body, the exact bytes posted: a body changed
// after signing fails the server's check.
func sign(body []byte) string {
	sum := sha1.Sum(body)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	var b strings.Builder
	b.WriteString("zzc")
	for _, i := range signPart1 {
		// 40 is past the hash, which the client skips.
		if i < len(h) {
			b.WriteByte(h[i])
		}
	}
	scrambled := make([]byte, len(signScramble))
	for i, v := range signScramble {
		scrambled[i] = v ^ sum[i]
	}
	b64 := base64.StdEncoding.EncodeToString(scrambled)
	b.WriteString(strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '+' || r == '=' {
			return -1
		}
		return r
	}, b64))
	for _, i := range signPart2 {
		b.WriteByte(h[i])
	}
	return strings.ToLower(b.String())
}
