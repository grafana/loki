// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
	"bytes"
	"unsafe"
)

// strnlen returns the length of s, but at most n. Like strlenIndexByte it
// scans in chunks ending at 4096-byte boundaries and never reads past s[n-1].
func strnlen(s uintptr, n Tsize_t) Tsize_t {
	p := s
	for n != 0 {
		chunk := 4096 - p%4096
		if uintptr(n) < chunk {
			chunk = uintptr(n)
		}
		if i := bytes.IndexByte(unsafe.Slice((*byte)(unsafe.Pointer(p)), chunk), 0); i >= 0 {
			return Tsize_t(p + uintptr(i) - s)
		}
		p += chunk
		n -= Tsize_t(chunk)
	}
	return Tsize_t(p - s)
}

// strncat appends at most n bytes of src plus a NUL to dest.
func strncat(dest, src uintptr, n Tsize_t) uintptr {
	// Short strings are cheaper to handle bytewise than to scan twice.
	d := dest
	for i := 0; i < 16 && *(*byte)(unsafe.Pointer(d)) != 0; i++ {
		d++
	}
	if *(*byte)(unsafe.Pointer(d)) != 0 {
		d += uintptr(strlen(d))
	}
	for i := 0; i < 16 && n != 0; i++ {
		c := *(*byte)(unsafe.Pointer(src))
		if c == 0 {
			break
		}

		*(*byte)(unsafe.Pointer(d)) = c
		d++
		src++
		n--
	}
	if n == 0 || *(*byte)(unsafe.Pointer(src)) == 0 {
		*(*byte)(unsafe.Pointer(d)) = 0
		return dest
	}

	k := strnlen(src, n)
	b := unsafe.Slice((*byte)(unsafe.Pointer(d)), k+1)
	copy(b, unsafe.Slice((*byte)(unsafe.Pointer(src)), k))
	b[k] = 0
	return dest
}
