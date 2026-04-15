// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package libc is a partial reimplementation of C libc in pure Go.
package libc // import "modernc.org/libc"

import (
	"math"
	"sync/atomic"
	"unsafe"

	"golang.org/x/exp/constraints"
)

func X__sync_add_and_fetch[T constraints.Integer](t *TLS, p uintptr, v T) T {
	switch unsafe.Sizeof(v) {
	case 4:
		return T(atomic.AddInt32((*int32)(unsafe.Pointer(p)), int32(v)))
	case 8:
		return T(atomic.AddInt64((*int64)(unsafe.Pointer(p)), int64(v)))
	default:
		panic(todo(""))
	}
}

func X__sync_sub_and_fetch[T constraints.Integer](t *TLS, p uintptr, v T) T {
	switch unsafe.Sizeof(v) {
	case 4:
		return T(atomic.AddInt32((*int32)(unsafe.Pointer(p)), -int32(v)))
	case 8:
		return T(atomic.AddInt64((*int64)(unsafe.Pointer(p)), -int64(v)))
	default:
		panic(todo(""))
	}
}

// GoString returns the value of a C string at s.
func GoString(s uintptr) string {
	if s == 0 {
		return ""
	}

	if n := strlen(s); n != 0 {
		return string(unsafe.Slice((*byte)(unsafe.Pointer(s)), n))
	}

	return ""
}

// GoBytes returns a byte slice from a C char* having length len bytes.
func GoBytes(s uintptr, len int) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(s)), len)
}

func X__isfinitef(tls *TLS, f float32) int32 {
	d := float64(f)
	if !math.IsInf(d, 0) && !math.IsNaN(d) {
		return 1
	}

	return 0
}

func X__isfinite(tls *TLS, d float64) int32 {
	if !math.IsInf(d, 0) && !math.IsNaN(d) {
		return 1
	}

	return 0
}

func X__isfinitel(tls *TLS, d float64) int32 {
	if !math.IsInf(d, 0) && !math.IsNaN(d) {
		return 1
	}

	return 0
}

func strlen(s uintptr) (r Tsize_t) {
	if s == 0 {
		return 0
	}

	for ; *(*int8)(unsafe.Pointer(s)) != 0; s++ {
		r++
	}

	return r
}

// size_t strlen(const char *s)
func Xstrlen(t *TLS, s uintptr) (r Tsize_t) {
	if __ccgo_strace {
		trc("t=%v s=%v, (%v:)", t, s, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	return strlen(s)

}

func _strlen(t *TLS, s uintptr) (r Tsize_t) {
	return strlen(s)
}
