// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
	"bytes"
	"math"
	"math/bits"
	"unsafe"
)

// strlen dispatches between the two implementations below via the
// build-tagged strlenUseIndexByte constant; see strlen_simd.go and
// strlen_nosimd.go. Both implementations may read (within the page
// holding the string) past the terminating NUL, but never past a page
// boundary they have no byte in, so neither can fault.
// TestStrlenGuardPage verifies that.

// strlenIndexByte scans with bytes.IndexByte over chunks that stop at
// 4096-byte boundaries. IndexByte never reads past its slice, and
// 4096 divides every supported page size, so the scan touches only
// pages the string occupies.
func strlenIndexByte(s uintptr) Tsize_t {
	p := s
	for {
		chunk := 4096 - p%4096
		b := unsafe.Slice((*byte)(unsafe.Pointer(p)), chunk)
		if i := bytes.IndexByte(b, 0); i >= 0 {
			return Tsize_t(p + uintptr(i) - s)
		}
		p += chunk
	}
}

const (
	strlenWordSize = bits.UintSize / 8
	strlenLowOnes  = math.MaxUint / 0xff // every byte 0x01
	strlenHighBits = strlenLowOnes << 7  // every byte 0x80
)

// strlenWords is the classic word-at-a-time scan: byte-wise to word
// alignment, then aligned native-word loads (which cannot cross a
// page boundary) with the usual zero-in-word bit test.
func strlenWords(s uintptr) Tsize_t {
	p := s
	for ; p%strlenWordSize != 0; p++ {
		if *(*byte)(unsafe.Pointer(p)) == 0 {
			return Tsize_t(p - s)
		}
	}
	for {
		x := *(*uint)(unsafe.Pointer(p))
		if (x-strlenLowOnes)&^x&strlenHighBits != 0 {
			break
		}
		p += strlenWordSize
	}
	for *(*byte)(unsafe.Pointer(p)) != 0 {
		p++
	}
	return Tsize_t(p - s)
}
