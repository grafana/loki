//go:build (!amd64 && !arm && !arm64) || appengine || !gc || noasm
// +build !amd64,!arm,!arm64 appengine !gc noasm

package lz4block

import (
	"encoding/binary"
)

func decodeBlock(dst, src, dict []byte) (ret int) {
	// Restrict capacities so we don't read or write out of bounds.
	dst = dst[:len(dst):len(dst)]
	src = src[:len(src):len(src)]

	const hasError = -2

	if len(src) == 0 {
		return hasError
	}

	defer func() {
		if recover() != nil {
			ret = hasError
		}
	}()

	var si, di uint
	for si < uint(len(src)) {
		// Literals and match lengths (token).
		b := uint(src[si])
		si++

		// Literals.
		if lLen := b >> 4; lLen > 0 {
			if lLen == 0xF {
				for {
					x := uint(src[si])
					if lLen += x; int(lLen) < 0 {
						return hasError
					}
					si++
					if x != 0xFF {
						break
					}
				}
			}
			if lLen <= 16 && si+16 < uint(len(src)) {
				// Shortcut 1: if we have enough room in src and dst, and the
				// literal length is at most 16, then copy 16 bytes, even if not
				// all are part of the literal. The compiler inlines this copy.
				copy(dst[di:di+16], src[si:si+16])
			} else {
				copy(dst[di:di+lLen], src[si:si+lLen])
			}
			si += lLen
			di += lLen
		}

		// Match.
		mLen := b & 0xF
		if si == uint(len(src)) && mLen == 0 {
			break
		} else if si >= uint(len(src)) {
			return hasError
		}
		mLen += minMatch

		offset := u16(src[si:])
		if offset == 0 {
			return hasError
		}
		si += 2

		if mLen <= 16 {
			// Shortcut 2: if the match length is at most 16 and we're far
			// enough from the end of dst, copy 16 bytes unconditionally
			// so that the compiler can inline the copy.
			if mLen <= offset && offset < di && di+16 <= uint(len(dst)) {
				i := di - offset
				copy(dst[di:di+16], dst[i:i+16])
				di += mLen
				continue
			}
		} else if mLen >= 15+minMatch {
			for {
				x := uint(src[si])
				if mLen += x; int(mLen) < 0 {
					return hasError
				}
				si++
				if x != 0xFF {
					break
				}
			}
		}

		// Copy the match.
		if di < offset {
			// The match is beyond our block, meaning the first part
			// is in the dictionary.
			fromDict := dict[uint(len(dict))+di-offset:]
			n := uint(copy(dst[di:di+mLen], fromDict))
			di += n
			if mLen -= n; mLen == 0 {
				continue
			}
			// We copied n = offset-di bytes from the dictionary,
			// then set di = di+n = offset, so the following code
			// copies from dst[di-offset:] = dst[0:].
		}

		expanded := dst[di-offset:]
		if mLen > offset {
			// Efficiently copy the match dst[di-offset:di] into the dst slice.
			bytesToCopy := offset * (mLen / offset)
			for n := offset; n <= bytesToCopy+offset; n *= 2 {
				copy(expanded[n:], expanded[:n])
			}
			di += bytesToCopy
			mLen -= bytesToCopy
		}
		di += uint(copy(dst[di:di+mLen], expanded[:mLen]))
	}

	return int(di)
}

func u16(p []byte) uint { return uint(binary.LittleEndian.Uint16(p)) }
