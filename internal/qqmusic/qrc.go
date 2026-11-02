package qqmusic

import "encoding/binary"

// QQ's lyrics are encrypted with a triple DES that differs from the
// standard's, as jixunmoe-go/qrc (MIT) worked out: ported from lyrune's
// port of it.

var qrcKeys = [3][8]byte{[8]byte([]byte("!@#)(*$%")), [8]byte([]byte("123ZXC!@")), [8]byte([]byte("!@#)(NHL"))}

// qrcDecrypt decrypts b, whole blocks of 8 bytes, in place.
func qrcDecrypt(b []byte) {
	stages := []*qrcDES{newQRCDES(qrcKeys[2], false), newQRCDES(qrcKeys[1], true), newQRCDES(qrcKeys[0], false)}
	for _, s := range stages {
		s.transform(b)
	}
}

// qrcEncrypt is qrcDecrypt undone, for tests.
func qrcEncrypt(b []byte) {
	stages := []*qrcDES{newQRCDES(qrcKeys[0], true), newQRCDES(qrcKeys[1], false), newQRCDES(qrcKeys[2], true)}
	for _, s := range stages {
		s.transform(b)
	}
}

type qrcDES struct{ subkeys [16]uint64 }

func newQRCDES(key [8]byte, encrypt bool) *qrcDES {
	permuted := mapU64(binary.LittleEndian.Uint64(key[:]), keyPermutation[:])
	c, d := uint32(permuted), uint32(permuted>>32)
	var s qrcDES
	for i, shift := range keyShifts {
		c = (c << shift) | ((c >> (28 - shift)) & 0xfffffff0)
		d = (d << shift) | ((d >> (28 - shift)) & 0xfffffff0)
		at := i
		if !encrypt {
			at = 15 - i
		}
		s.subkeys[at] = mapU64(makeU64(d, c), keyCompression[:])
	}
	return &s
}

func (s *qrcDES) transform(b []byte) {
	for i := 0; i+8 <= len(b); i += 8 {
		binary.LittleEndian.PutUint64(b[i:], s.block(binary.LittleEndian.Uint64(b[i:])))
	}
}

func (s *qrcDES) block(data uint64) uint64 {
	state := mapU64(data, initialPermutation[:])
	for _, k := range s.subkeys {
		state = cryptRound(state, k)
	}
	return mapU64(state<<32|state>>32, inversePermutation[:])
}

func cryptRound(state, key uint64) uint64 {
	high, low := uint32(state>>32), uint32(state)
	expanded := mapU64(makeU64(high, high), keyExpansion[:]) ^ key
	var sub uint32
	for i, shift := range sboxShifts {
		sub = sub<<4 | uint32(sboxes[i][(expanded>>shift)&0x3f])
	}
	return makeU64(mapU32(sub, pBox[:])^low, high)
}

func makeU64(high, low uint32) uint64 { return uint64(high)<<32 | uint64(low) }

func bitMask(i uint8) uint64 {
	if i < 32 {
		return 1 << (31 - i)
	}
	return 1 << (95 - uint64(i))
}

func mapU32(src uint32, table []uint8) uint32 {
	var out uint64
	for i, from := range table {
		if bitMask(from)&uint64(src) != 0 {
			out |= bitMask(uint8(i))
		}
	}
	return uint32(out)
}

func mapU64(src uint64, table []uint8) uint64 {
	mid := len(table) / 2
	var lo, hi uint64
	for i, from := range table[:mid] {
		if bitMask(from)&src != 0 {
			lo |= bitMask(uint8(i))
		}
	}
	for i, from := range table[mid:] {
		if bitMask(from)&src != 0 {
			hi |= bitMask(uint8(i))
		}
	}
	return makeU64(uint32(hi), uint32(lo))
}

var keyShifts = [16]uint8{1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1}
var sboxShifts = [8]uint8{26, 20, 14, 8, 58, 52, 46, 40}

var sboxes = [8][64]uint8{
	{14, 0, 4, 15, 13, 7, 1, 4, 2, 14, 15, 2, 11, 13, 8, 1, 3, 10, 10, 6, 6, 12, 12, 11, 5, 9, 9, 5, 0, 3, 7, 8, 4, 15, 1, 12, 14, 8, 8, 2, 13, 4, 6, 9, 2, 1, 11, 7, 15, 5, 12, 11, 9, 3, 7, 14, 3, 10, 10, 0, 5, 6, 0, 13},
	{15, 3, 1, 13, 8, 4, 14, 7, 6, 15, 11, 2, 3, 8, 4, 15, 9, 12, 7, 0, 2, 1, 13, 10, 12, 6, 0, 9, 5, 11, 10, 5, 0, 13, 14, 8, 7, 10, 11, 1, 10, 3, 4, 15, 13, 4, 1, 2, 5, 11, 8, 6, 12, 7, 6, 12, 9, 0, 3, 5, 2, 14, 15, 9},
	{10, 13, 0, 7, 9, 0, 14, 9, 6, 3, 3, 4, 15, 6, 5, 10, 1, 2, 13, 8, 12, 5, 7, 14, 11, 12, 4, 11, 2, 15, 8, 1, 13, 1, 6, 10, 4, 13, 9, 0, 8, 6, 15, 9, 3, 8, 0, 7, 11, 4, 1, 15, 2, 14, 12, 3, 5, 11, 10, 5, 14, 2, 7, 12},
	{7, 13, 13, 8, 14, 11, 3, 5, 0, 6, 6, 15, 9, 0, 10, 3, 1, 4, 2, 7, 8, 2, 5, 12, 11, 1, 12, 10, 4, 14, 15, 9, 10, 3, 6, 15, 9, 0, 0, 6, 12, 10, 11, 10, 7, 13, 13, 8, 15, 9, 1, 4, 3, 5, 14, 11, 5, 12, 2, 7, 8, 2, 4, 14},
	{2, 14, 12, 11, 4, 2, 1, 12, 7, 4, 10, 7, 11, 13, 6, 1, 8, 5, 5, 0, 3, 15, 15, 10, 13, 3, 0, 9, 14, 8, 9, 6, 4, 11, 2, 8, 1, 12, 11, 7, 10, 1, 13, 14, 7, 2, 8, 13, 15, 6, 9, 15, 12, 0, 5, 9, 6, 10, 3, 4, 0, 5, 14, 3},
	{12, 10, 1, 15, 10, 4, 15, 2, 9, 7, 2, 12, 6, 9, 8, 5, 0, 6, 13, 1, 3, 13, 4, 14, 14, 0, 7, 11, 5, 3, 11, 8, 9, 4, 14, 3, 15, 2, 5, 12, 2, 9, 8, 5, 12, 15, 3, 10, 7, 11, 0, 14, 4, 1, 10, 7, 1, 6, 13, 0, 11, 8, 6, 13},
	{4, 13, 11, 0, 2, 11, 14, 7, 15, 4, 0, 9, 8, 1, 13, 10, 3, 14, 12, 3, 9, 5, 7, 12, 5, 2, 10, 15, 6, 8, 1, 6, 1, 6, 4, 11, 11, 13, 13, 8, 12, 1, 3, 4, 7, 10, 14, 7, 10, 9, 15, 5, 6, 0, 8, 15, 0, 14, 5, 2, 9, 3, 2, 12},
	{13, 1, 2, 15, 8, 13, 4, 8, 6, 10, 15, 3, 11, 7, 1, 4, 10, 12, 9, 5, 3, 6, 14, 11, 5, 0, 0, 14, 12, 9, 7, 2, 7, 2, 11, 1, 4, 14, 1, 7, 9, 4, 12, 10, 14, 8, 2, 13, 0, 15, 6, 12, 10, 9, 13, 0, 15, 3, 3, 5, 5, 6, 8, 11},
}

var pBox = [32]uint8{15, 6, 19, 20, 28, 11, 27, 16, 0, 14, 22, 25, 4, 17, 30, 9, 1, 7, 23, 13, 31, 26, 2, 8, 18, 12, 29, 5, 21, 10, 3, 24}

var initialPermutation = [64]uint8{57, 49, 41, 33, 25, 17, 9, 1, 59, 51, 43, 35, 27, 19, 11, 3, 61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7, 56, 48, 40, 32, 24, 16, 8, 0, 58, 50, 42, 34, 26, 18, 10, 2, 60, 52, 44, 36, 28, 20, 12, 4, 62, 54, 46, 38, 30, 22, 14, 6}

var inversePermutation = [64]uint8{39, 7, 47, 15, 55, 23, 63, 31, 38, 6, 46, 14, 54, 22, 62, 30, 37, 5, 45, 13, 53, 21, 61, 29, 36, 4, 44, 12, 52, 20, 60, 28, 35, 3, 43, 11, 51, 19, 59, 27, 34, 2, 42, 10, 50, 18, 58, 26, 33, 1, 41, 9, 49, 17, 57, 25, 32, 0, 40, 8, 48, 16, 56, 24}

var keyPermutation = [56]uint8{56, 48, 40, 32, 24, 16, 8, 0, 57, 49, 41, 33, 25, 17, 9, 1, 58, 50, 42, 34, 26, 18, 10, 2, 59, 51, 43, 35, 62, 54, 46, 38, 30, 22, 14, 6, 61, 53, 45, 37, 29, 21, 13, 5, 60, 52, 44, 36, 28, 20, 12, 4, 27, 19, 11, 3}

var keyCompression = [48]uint8{13, 16, 10, 23, 0, 4, 2, 27, 14, 5, 20, 9, 22, 18, 11, 3, 25, 7, 15, 6, 26, 19, 12, 1, 45, 56, 35, 41, 51, 59, 34, 44, 55, 49, 37, 52, 48, 53, 43, 60, 38, 57, 50, 46, 54, 40, 33, 36}

var keyExpansion = [48]uint8{31, 0, 1, 2, 3, 4, 3, 4, 5, 6, 7, 8, 7, 8, 9, 10, 11, 12, 11, 12, 13, 14, 15, 16, 15, 16, 17, 18, 19, 20, 19, 20, 21, 22, 23, 24, 23, 24, 25, 26, 27, 28, 27, 28, 29, 30, 31, 0}
