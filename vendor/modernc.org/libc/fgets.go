// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!linux || mips64le) && !darwin

package libc // import "modernc.org/libc"

import (
	"unsafe"

	"modernc.org/libc/stdio"
)

// char *fgets(char *s, int size, FILE *stream);
func Xfgets(t *TLS, s uintptr, size int32, stream uintptr) uintptr {
	if __ccgo_strace {
		trc("t=%v s=%v size=%v stream=%v, (%v:)", t, s, size, stream, origin(2))
	}
	if size < 1 {
		return 0
	}
	if size == 1 {
		*(*byte)(unsafe.Pointer(s)) = 0
		return s
	}
	var b []byte
out:
	for ; size > 1; size-- {
		switch c := Xfgetc(t, stream); c {
		case '\n':
			b = append(b, byte(c))
			break out
		case stdio.EOF:
			break out
		default:
			b = append(b, byte(c))
		}
	}
	if len(b) == 0 {
		return 0
	}

	b = append(b, 0)
	copy((*RawMem)(unsafe.Pointer(s))[:len(b):len(b)], b)
	return s
}
