// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
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
