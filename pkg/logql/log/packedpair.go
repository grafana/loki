package log

import "bytes"

// Packed-pair substring search, ported from Rust memchr's memmem finder.
//
// The rank table below is copied from memchr (Copyright (c) 2015 Andrew Gallant),
// which is licensed under MIT OR Unlicense:
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in
//	all copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
//	THE SOFTWARE.
//
// A lower rank means the byte is believed to occur less often in typical text.
// https://github.com/BurntSushi/memchr

// packedPairMaxLen is the longest needle searched with the rare-byte pair.
// Longer needles stay on bytes.Contains. memchr uses the same cutoff: confirming
// a long needle at every false pair hit is slower than Two-Way or Rabin-Karp,
// and the worst case is linear in needle length times haystack length.
const packedPairMaxLen = 32

// byteRank[b] is the heuristic frequency of byte b. Lower is rarer.
var byteRank = [256]byte{
	55, 52, 51, 50, 49, 48, 47, 46, 45, 103, 242, 66, 67, 229, 44, 43,
	42, 41, 40, 39, 38, 37, 36, 35, 34, 33, 56, 32, 31, 30, 29, 28,
	255, 148, 164, 149, 136, 160, 155, 173, 221, 222, 134, 122, 232, 202, 215, 224,
	208, 220, 204, 187, 183, 179, 177, 168, 178, 200, 226, 195, 154, 184, 174, 126,
	120, 191, 157, 194, 170, 189, 162, 161, 150, 193, 142, 137, 171, 176, 185, 167,
	186, 112, 175, 192, 188, 156, 140, 143, 123, 133, 128, 147, 138, 146, 114, 223,
	151, 249, 216, 238, 236, 253, 227, 218, 230, 247, 135, 180, 241, 233, 246, 244,
	231, 139, 245, 243, 251, 235, 201, 196, 240, 214, 152, 182, 205, 181, 127, 27,
	212, 211, 210, 213, 228, 197, 169, 159, 131, 172, 105, 80, 98, 96, 97, 81,
	207, 145, 116, 115, 144, 130, 153, 121, 107, 132, 109, 110, 124, 111, 82, 108,
	118, 141, 113, 129, 119, 125, 165, 117, 92, 106, 83, 72, 99, 93, 65, 79,
	166, 237, 163, 199, 190, 225, 209, 203, 198, 217, 219, 206, 234, 248, 158, 239,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

// substrFinder reports whether a needle occurs in a log line.
// A packed finder scans for the needle's two rarest bytes, then confirms with
// bytes.Equal. Other needles use bytes.Contains.
type substrFinder struct {
	needle []byte
	packed bool
	index1 int
	index2 int
	byte1  byte
	byte2  byte
}

// newSubstrFinder builds a searcher for needle. The same needle should be
// reused across lines; construction picks the byte pair once.
func newSubstrFinder(needle []byte) substrFinder {
	if len(needle) < 2 || len(needle) > packedPairMaxLen {
		return substrFinder{needle: needle}
	}
	index1, index2 := rarePair(needle)
	return substrFinder{
		needle: needle,
		packed: true,
		index1: index1,
		index2: index2,
		byte1:  needle[index1],
		byte2:  needle[index2],
	}
}

// rarePair returns the offsets of the two rarest bytes in needle.
// needle must contain at least two bytes. Offsets past 254 are ignored,
// matching memchr, which stores the offsets in a byte.
func rarePair(needle []byte) (int, int) {
	rare1, index1 := needle[0], 0
	rare2, index2 := needle[1], 1
	if byteRank[rare2] < byteRank[rare1] {
		rare1, rare2 = rare2, rare1
		index1, index2 = index2, index1
	}
	limit := len(needle)
	if limit > 255 {
		limit = 255
	}
	for i := 2; i < limit; i++ {
		b := needle[i]
		rank := byteRank[b]
		if rank < byteRank[rare1] {
			rare2, index2 = rare1, index1
			rare1, index1 = b, i
		} else if b != rare1 && rank < byteRank[rare2] {
			rare2, index2 = b, i
		}
	}
	return index1, index2
}

// contains reports whether the needle occurs in haystack.
func (f substrFinder) contains(haystack []byte) bool {
	needle := f.needle
	if !f.packed {
		return bytes.Contains(haystack, needle)
	}
	n := len(needle)
	if len(haystack) < n {
		return false
	}
	// byte1 cannot start a match past this index.
	max := len(haystack) - n + f.index1
	i := f.index1
	for i <= max {
		rel := bytes.IndexByte(haystack[i:], f.byte1)
		if rel < 0 {
			return false
		}
		found := i + rel
		if found > max {
			return false
		}
		start := found - f.index1
		if haystack[start+f.index2] == f.byte2 && bytes.Equal(haystack[start:start+n], needle) {
			return true
		}
		i = found + 1
	}
	return false
}
