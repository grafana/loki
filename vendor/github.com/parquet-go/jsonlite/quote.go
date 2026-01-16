package jsonlite

import (
	"math/bits"
	"unsafe"
)

// Quote returns s as a JSON quoted string.
func Quote(s string) string {
	return string(AppendQuote(make([]byte, 0, 2+len(s)), s))
}

// AppendQuote appends a JSON quoted string to b and returns the result.
func AppendQuote(b []byte, s string) []byte {
	b = append(b, '"')

	for {
		i := escapeIndex(s)
		if i < 0 {
			b = append(b, s...)
			break
		}

		b = append(b, s[:i]...)
		switch c := s[i]; c {
		case '\\', '"':
			b = append(b, '\\', c)
		case '\b':
			b = append(b, '\\', 'b')
		case '\f':
			b = append(b, '\\', 'f')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			const hex = "0123456789abcdef"
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		}

		s = s[i+1:]
	}

	return append(b, '"')
}

/*
MIT License

Copyright (c) 2019 Segment.io, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

const (
	lsb = 0x0101010101010101
	msb = 0x8080808080808080
)

// escapeIndex finds the index of the first char in `s` that requires escaping.
// A char requires escaping if it's outside of the range of [0x20, 0x7F] or if
// it includes a double quote or backslash. If no chars in `s` require escaping,
// the return value is -1.
func escapeIndex(s string) int {
	var i int
	if len(s) >= 8 {
		chunks := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.StringData(s))), len(s)/8)
		for j, n := range chunks {
			// combine masks before checking for the MSB of each byte. We include
			// `n` in the mask to check whether any of the *input* byte MSBs were
			// set (i.e. the byte was outside the ASCII range).
			mask := n | below(n, 0x20) | contains(n, '"') | contains(n, '\\')
			if (mask & msb) != 0 {
				return j*8 + bits.TrailingZeros64(mask&msb)/8
			}
		}
		i = len(chunks) * 8
	}

	for ; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7f || c == '"' || c == '\\' {
			return i
		}
	}

	return -1
}

// below return a mask that can be used to determine if any of the bytes
// in `n` are below `b`. If a byte's MSB is set in the mask then that byte was
// below `b`. The result is only valid if `b`, and each byte in `n`, is below
// 0x80.
func below(n uint64, b byte) uint64 { return n - expand(b) }

// contains returns a mask that can be used to determine if any of the
// bytes in `n` are equal to `b`. If a byte's MSB is set in the mask then
// that byte is equal to `b`. The result is only valid if `b`, and each
// byte in `n`, is below 0x80.
func contains(n uint64, b byte) uint64 { return (n ^ expand(b)) - lsb }

// expand puts the specified byte into each of the 8 bytes of a uint64.
func expand(b byte) uint64 { return lsb * uint64(b) }
