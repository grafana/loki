// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The netbsd/amd64, illumos/amd64 and linux/mips64le ports define these
// functions in their own files.

//go:build !linux && !(netbsd && amd64) && !(illumos && amd64)

package libc // import "modernc.org/libc"

func AtomicLoadPInt8(addr uintptr) (val int8) {
	return int8(a_load_8(addr))
}

func AtomicLoadPInt16(addr uintptr) (val int16) {
	return int16(a_load_16(addr))
}

func AtomicLoadPUint8(addr uintptr) byte {
	return byte(a_load_8(addr))
}

func AtomicLoadPUint16(addr uintptr) uint16 {
	return uint16(a_load_16(addr))
}
