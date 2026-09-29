// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)

package libc // import "modernc.org/libc"

import (
	"bytes"
	"math"
	"math/bits"
	"unsafe"
)

// This file replaces hot transpiled musl routines with native Go
// implementations on every musl-derived (linux) target. The transpiled
// mem* routines move at most four bytes per load/store; the Go
// runtime's memmove, memclr, and bytes.Compare use much wider (SIMD)
// operations. The transpiled originals are kept in the generated
// ccgo_linux_<goarch>.go files under ___musl_* names; generator.go
// renames them as part of generation, so make generate maintains this
// arrangement. (On linux/arm musl's memcpy is assembly and was never
// transpiled; the hand-written memcpy in libc_musl_linux_arm.go plays
// the ___musl_memcpy role there.) Should the two ever fall out of
// sync, the build fails with duplicate or missing definitions rather
// than silently reverting. native_musl_test.go verifies these against
// the ___musl_* originals and benchmarks them; the choices below are
// those measurements' winners on linux/amd64 and linux/arm64,
// reworked to use native-word operations where the originals leaned
// on 64-bit ones, so that the 32-bit targets are not penalized.

// void *memcpy(void *dest, const void *src, size_t n);
func Xmemcpy(tls *TLS, dest uintptr, src uintptr, n Tsize_t) (r uintptr) {
	if __ccgo_strace {
		trc("tls=%v dest=%v src=%v n=%v, (%v:)", tls, dest, src, n, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	if n != 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(dest)), n), unsafe.Slice((*byte)(unsafe.Pointer(src)), n))
	}
	return dest
}

// void *memmove(void *dest, const void *src, size_t n);
func Xmemmove(tls *TLS, dest uintptr, src uintptr, n Tsize_t) (r uintptr) {
	if __ccgo_strace {
		trc("tls=%v dest=%v src=%v n=%v, (%v:)", tls, dest, src, n, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	if n != 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(dest)), n), unsafe.Slice((*byte)(unsafe.Pointer(src)), n))
	}
	return dest
}

// void *memset(void *s, int c, size_t n);
func Xmemset(tls *TLS, dest uintptr, c int32, n Tsize_t) (r uintptr) {
	if __ccgo_strace {
		trc("tls=%v dest=%v c=%v n=%v, (%v:)", tls, dest, c, n, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	if n == 0 {
		return dest
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(dest)), n)
	bc := byte(c)
	if bc == 0 {
		clear(b)
		return dest
	}
	if len(b) < 8 {
		for i := range b {
			b[i] = bc
		}
		return dest
	}
	// Fill the first min(n, 512) bytes: byte stores up to the first
	// word-aligned address, then aligned native-word stores of the
	// replicated fill byte (fast and alignment-safe on every arch,
	// and free of 64-bit arithmetic, which is expensive on the 32-bit
	// targets), then repeatedly double the filled prefix with copy,
	// which runs as the runtime's vectorized memmove.
	const ws = bits.UintSize / 8
	x := uint(math.MaxUint/0xff) * uint(bc) // bc replicated into every byte
	seed := min(len(b), 512)
	i := 0
	for ; i < seed && uintptr(unsafe.Pointer(&b[i]))%ws != 0; i++ {
		b[i] = bc
	}
	for ; i+4*ws <= seed; i += 4 * ws {
		q := (*[4]uint)(unsafe.Pointer(&b[i]))
		q[0] = x
		q[1] = x
		q[2] = x
		q[3] = x
	}
	for ; i+ws <= seed; i += ws {
		*(*uint)(unsafe.Pointer(&b[i])) = x
	}
	for ; i < seed; i++ {
		b[i] = bc
	}
	for i := seed; i < len(b); {
		i += copy(b[i:], b[:i]) // doubles i without ever overflowing int
	}
	return dest
}

// int memcmp(const void *vl, const void *vr, size_t n);
func Xmemcmp(tls *TLS, vl uintptr, vr uintptr, n Tsize_t) (r1 int32) {
	if __ccgo_strace {
		trc("tls=%v vl=%v vr=%v n=%v, (%v:)", tls, vl, vr, n, origin(2))
		defer func() { trc("-> %v", r1) }()
	}
	if n == 0 {
		return 0
	}
	l := unsafe.Slice((*byte)(unsafe.Pointer(vl)), n)
	r := unsafe.Slice((*byte)(unsafe.Pointer(vr)), n)
	// bytes.Compare returns -1/0/+1 rather than musl's difference of
	// the first mismatched bytes; C99 7.21.4 specifies only the sign.
	return int32(bytes.Compare(l, r))
}

// double fabs(double x);
func Xfabs(tls *TLS, x float64) (r float64) {
	if __ccgo_strace {
		trc("tls=%v x=%v, (%v:)", tls, x, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	r = math.Abs(x)
	if tls.checkSignals {
		tls.checkSignal()
	}
	return r
}

// size_t strcspn(const char *s, const char *c);
func Xstrcspn(tls *TLS, s uintptr, c uintptr) (r Tsize_t) {
	if __ccgo_strace {
		trc("tls=%v s=%v c=%v, (%v:)", tls, s, c, origin(2))
		defer func() { trc("-> %v", r) }()
	}
	// Native words rather than uint64 so that the 32-bit targets do
	// not pay for 64-bit shifts on every byte.
	var set [256 / bits.UintSize]uint
	for p := c; ; p++ {
		ch := *(*byte)(unsafe.Pointer(p))
		if ch == 0 {
			break
		}
		set[ch/bits.UintSize] |= 1 << (ch % bits.UintSize)
	}
	for q := s; ; q++ {
		ch := *(*byte)(unsafe.Pointer(q))
		if ch == 0 || set[ch/bits.UintSize]&(1<<(ch%bits.UintSize)) != 0 {
			r = Tsize_t(q - s)
			if tls.checkSignals {
				tls.checkSignal()
			}
			return r
		}
	}
}
