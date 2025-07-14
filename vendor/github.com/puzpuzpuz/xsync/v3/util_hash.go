package xsync

import (
	"reflect"
	"unsafe"
)

// makeSeed creates a random seed.
func makeSeed() uint64 {
	var s1 uint32
	for {
		s1 = runtime_fastrand()
		// We use seed 0 to indicate an uninitialized seed/hash,
		// so keep trying until we get a non-zero seed.
		if s1 != 0 {
			break
		}
	}
	s2 := runtime_fastrand()
	return uint64(s1)<<32 | uint64(s2)
}

// hashString calculates a hash of s with the given seed.
func hashString(s string, seed uint64) uint64 {
	if s == "" {
		return seed
	}
	strh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	return uint64(runtime_memhash(unsafe.Pointer(strh.Data), uintptr(seed), uintptr(strh.Len)))
}

//go:noescape
//go:linkname runtime_memhash runtime.memhash
func runtime_memhash(p unsafe.Pointer, h, s uintptr) uintptr

// defaultHasher creates a fast hash function for the given comparable type.
// The only limitation is that the type should not contain interfaces inside
// based on runtime.typehash.
func defaultHasher[T comparable]() func(T, uint64) uint64 {
	var zero T

	if reflect.TypeOf(&zero).Elem().Kind() == reflect.Interface {
		return func(value T, seed uint64) uint64 {
			iValue := any(value)
			i := (*iface)(unsafe.Pointer(&iValue))
			return runtime_typehash64(i.typ, i.word, seed)
		}
	} else {
		var iZero any = zero
		i := (*iface)(unsafe.Pointer(&iZero))
		return func(value T, seed uint64) uint64 {
			return runtime_typehash64(i.typ, unsafe.Pointer(&value), seed)
		}
	}
}

// how interface is represented in memory
type iface struct {
	typ  uintptr
	word unsafe.Pointer
}

// same as runtime_typehash, but always returns a uint64
// see: maphash.rthash function for details
func runtime_typehash64(t uintptr, p unsafe.Pointer, seed uint64) uint64 {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return uint64(runtime_typehash(t, p, uintptr(seed)))
	}

	lo := runtime_typehash(t, p, uintptr(seed))
	hi := runtime_typehash(t, p, uintptr(seed>>32))
	return uint64(hi)<<32 | uint64(lo)
}

//go:noescape
//go:linkname runtime_typehash runtime.typehash
func runtime_typehash(t uintptr, p unsafe.Pointer, h uintptr) uintptr
