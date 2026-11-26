// Copyright 2026 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libc // import "modernc.org/libc"

import (
	"sort"
	"unsafe"
)

// qsort sorts the nmemb elements of size bytes at base with compar, shared
// by both paths: sort.Sort is pdqsort, which calls compar far fewer times
// than musl's smoothsort, and elements are swapped a word at a time.
func qsort(t *TLS, base uintptr, nmemb, size uintptr, compar uintptr) {
	if nmemb < 2 || size == 0 {
		return
	}

	s := &sorter{
		len:  int(nmemb),
		base: base,
		sz:   size,
		f: (*struct {
			f func(*TLS, uintptr, uintptr) int32
		})(unsafe.Pointer(&struct{ uintptr }{compar})).f,
		t: t,
	}
	// The widest word that both base and size are multiples of: C
	// guarantees elements aligned for their type, nothing more.
	switch {
	case base%8 == 0 && size%8 == 0:
		s.word = 8
	case base%4 == 0 && size%4 == 0:
		s.word = 4
	default:
		s.word = 1
	}
	sort.Sort(s)
}

type sorter struct {
	len  int
	base uintptr
	sz   uintptr
	word uintptr
	f    func(*TLS, uintptr, uintptr) int32
	t    *TLS
}

func (s *sorter) Len() int { return s.len }

func (s *sorter) Less(i, j int) bool {
	return s.f(s.t, s.base+uintptr(i)*s.sz, s.base+uintptr(j)*s.sz) < 0
}

func (s *sorter) Swap(i, j int) {
	p := s.base + uintptr(i)*s.sz
	q := s.base + uintptr(j)*s.sz
	n := s.sz
	switch s.word {
	case 8:
		for ; n >= 8; n -= 8 {
			*(*uint64)(unsafe.Pointer(p)), *(*uint64)(unsafe.Pointer(q)) = *(*uint64)(unsafe.Pointer(q)), *(*uint64)(unsafe.Pointer(p))
			p += 8
			q += 8
		}
	case 4:
		for ; n >= 4; n -= 4 {
			*(*uint32)(unsafe.Pointer(p)), *(*uint32)(unsafe.Pointer(q)) = *(*uint32)(unsafe.Pointer(q)), *(*uint32)(unsafe.Pointer(p))
			p += 4
			q += 4
		}
	}
	for ; n != 0; n-- {
		*(*byte)(unsafe.Pointer(p)), *(*byte)(unsafe.Pointer(q)) = *(*byte)(unsafe.Pointer(q)), *(*byte)(unsafe.Pointer(p))
		p++
		q++
	}
}
