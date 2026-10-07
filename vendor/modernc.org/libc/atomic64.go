// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64)

package libc // import "modernc.org/libc"

import (
	mbits "math/bits"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// static inline int a_ctz_l(unsigned long x)
func _a_ctz_l(tls *TLS, x ulong) int32 {
	return int32(mbits.TrailingZeros64(x))
}

// The functions below implement the atomic primitives that the arch specific
// atomic_arch.h overlays (internal/overlay/musl/arch/*/atomic_arch.h) only
// declare, because ccgo cannot translate their inline assembly. atomic.h
// derives the remaining primitives from these in C. See
// https://gitlab.com/cznic/libc/-/issues/53.

// static inline int a_cas(volatile int *p, int t, int s)
func _a_cas(tls *TLS, p uintptr, t, s int32) int32 {
	return casInt32(p, t, s)
}

// static inline void *a_cas_p(volatile void *p, void *t, void *s)
func _a_cas_p(tls *TLS, p, t, s uintptr) uintptr {
	w := (*uintptr)(unsafe.Pointer(p))
	for {
		old := atomic.LoadUintptr(w)
		if old != t {
			return old
		}

		if atomic.CompareAndSwapUintptr(w, t, s) {
			return t
		}
	}
}

// static inline int a_swap(volatile int *p, int v)
func _a_swap(tls *TLS, p uintptr, v int32) int32 {
	return atomic.SwapInt32((*int32)(unsafe.Pointer(p)), v)
}

// static inline int a_fetch_add(volatile int *p, int v)
func _a_fetch_add(tls *TLS, p uintptr, v int32) int32 {
	return atomic.AddInt32((*int32)(unsafe.Pointer(p)), v) - v
}

// static inline void a_and(volatile int *p, int v)
func _a_and(tls *TLS, p uintptr, v int32) {
	w := (*int32)(unsafe.Pointer(p))
	for {
		old := atomic.LoadInt32(w)
		if atomic.CompareAndSwapInt32(w, old, old&v) {
			return
		}
	}
}

// static inline void a_or(volatile int *p, int v)
func _a_or(tls *TLS, p uintptr, v int32) {
	w := (*int32)(unsafe.Pointer(p))
	for {
		old := atomic.LoadInt32(w)
		if atomic.CompareAndSwapInt32(w, old, old|v) {
			return
		}
	}
}

// static inline void a_and_64(volatile uint64_t *p, uint64_t v)
func _a_and_64(tls *TLS, p uintptr, v uint64) {
	w := (*uint64)(unsafe.Pointer(p))
	for {
		old := atomic.LoadUint64(w)
		if atomic.CompareAndSwapUint64(w, old, old&v) {
			return
		}
	}
}

// static inline void a_or_64(volatile uint64_t *p, uint64_t v)
func _a_or_64(tls *TLS, p uintptr, v uint64) {
	w := (*uint64)(unsafe.Pointer(p))
	for {
		old := atomic.LoadUint64(w)
		if atomic.CompareAndSwapUint64(w, old, old|v) {
			return
		}
	}
}

// static inline void a_store(volatile int *p, int x)
func _a_store(tls *TLS, p uintptr, x int32) {
	atomic.StoreInt32((*int32)(unsafe.Pointer(p)), x)
}

var atomicBarrier atomic.Int32

// static inline void a_barrier(void)
func _a_barrier(tls *TLS) {
	atomicBarrier.Add(1)
}

// static inline void a_spin(void)
//
// Spinning callers wait for another C thread, which is a goroutine here, so
// yield instead of burning the P.
func _a_spin(tls *TLS) {
	runtime.Gosched()
}

// static inline void a_crash(void)
func _a_crash(tls *TLS) {
	panic("crash")
}

// static inline int a_clz_64(uint64_t x)
func _a_clz_64(tls *TLS, x uint64) int32 {
	return int32(mbits.LeadingZeros64(x))
}
