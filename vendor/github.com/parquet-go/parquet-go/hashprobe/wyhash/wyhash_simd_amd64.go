//go:build !purego && goexperiment.simd

package wyhash

import (
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

// The assembly versions of these kernels are scalar MULQ loops over strided
// pointers; the loops below compile to the same two MULQ per value
// (bits.Mul64 is an intrinsic). The seed is mixed into the constants once,
// the strided pointer is advanced additively (indexing would cost an IMUL
// per element), and the hashes are written through [4]uintptr chunk views
// because the prove pass cannot derive hashes[i+3] from i+4 <= len(hashes).

func MultiHashUint32Array(hashes []uintptr, values sparse.Uint32Array, seed uintptr) {
	a := values.UnsafeArray()
	n := len(hashes)
	if n == 0 {
		return
	}
	m1s := m1 ^ uint64(seed)
	p := a.Index(0)
	off := arrayStride(a, n)
	c := unsafecast.Slice[[4]uintptr](hashes)
	for j := range c {
		q := unsafe.Add(p, uintptr(4*j)*off)
		v0 := uint64(*(*uint32)(q))
		v1 := uint64(*(*uint32)(unsafe.Add(q, off)))
		v2 := uint64(*(*uint32)(unsafe.Add(q, 2*off)))
		v3 := uint64(*(*uint32)(unsafe.Add(q, 3*off)))
		h := &c[j]
		h[0] = uintptr(mix(m5^4, mix(v0^m2, v0^m1s)))
		h[1] = uintptr(mix(m5^4, mix(v1^m2, v1^m1s)))
		h[2] = uintptr(mix(m5^4, mix(v2^m2, v2^m1s)))
		h[3] = uintptr(mix(m5^4, mix(v3^m2, v3^m1s)))
	}
	for i := len(c) * 4; i < n; i++ {
		v := uint64(*(*uint32)(unsafe.Add(p, uintptr(i)*off)))
		hashes[i] = uintptr(mix(m5^4, mix(v^m2, v^m1s)))
	}
}

func MultiHashUint64Array(hashes []uintptr, values sparse.Uint64Array, seed uintptr) {
	a := values.UnsafeArray()
	n := len(hashes)
	if n == 0 {
		return
	}
	m1s := m1 ^ uint64(seed)
	p := a.Index(0)
	off := arrayStride(a, n)
	c := unsafecast.Slice[[4]uintptr](hashes)
	for j := range c {
		q := unsafe.Add(p, uintptr(4*j)*off)
		v0 := *(*uint64)(q)
		v1 := *(*uint64)(unsafe.Add(q, off))
		v2 := *(*uint64)(unsafe.Add(q, 2*off))
		v3 := *(*uint64)(unsafe.Add(q, 3*off))
		h := &c[j]
		h[0] = uintptr(mix(m5^8, mix(v0^m2, v0^m1s)))
		h[1] = uintptr(mix(m5^8, mix(v1^m2, v1^m1s)))
		h[2] = uintptr(mix(m5^8, mix(v2^m2, v2^m1s)))
		h[3] = uintptr(mix(m5^8, mix(v3^m2, v3^m1s)))
	}
	for i := len(c) * 4; i < n; i++ {
		v := *(*uint64)(unsafe.Add(p, uintptr(i)*off))
		hashes[i] = uintptr(mix(m5^8, mix(v^m2, v^m1s)))
	}
}

func MultiHashUint128Array(hashes []uintptr, values sparse.Uint128Array, seed uintptr) {
	a := values.UnsafeArray()
	n := len(hashes)
	if n == 0 {
		return
	}
	m1s := m1 ^ uint64(seed)
	p := a.Index(0)
	off := arrayStride(a, n)
	c := unsafecast.Slice[[4]uintptr](hashes)
	for j := range c {
		q := unsafe.Add(p, uintptr(4*j)*off)
		p0, p1, p2, p3 := q, unsafe.Add(q, off), unsafe.Add(q, 2*off), unsafe.Add(q, 3*off)
		h := &c[j]
		h[0] = uintptr(mix(m5^16, mix(*(*uint64)(p0)^m2, *(*uint64)(unsafe.Add(p0, 8))^m1s)))
		h[1] = uintptr(mix(m5^16, mix(*(*uint64)(p1)^m2, *(*uint64)(unsafe.Add(p1, 8))^m1s)))
		h[2] = uintptr(mix(m5^16, mix(*(*uint64)(p2)^m2, *(*uint64)(unsafe.Add(p2, 8))^m1s)))
		h[3] = uintptr(mix(m5^16, mix(*(*uint64)(p3)^m2, *(*uint64)(unsafe.Add(p3, 8))^m1s)))
	}
	for i := len(c) * 4; i < n; i++ {
		q := unsafe.Add(p, uintptr(i)*off)
		h := mix(m5^16, mix(*(*uint64)(q)^m2, *(*uint64)(unsafe.Add(q, 8))^m1s))
		hashes[i] = uintptr(h)
	}
}

func arrayStride(a sparse.Array, n int) uintptr {
	if n > 1 {
		return uintptr(a.Index(1)) - uintptr(a.Index(0))
	}
	return 0
}
