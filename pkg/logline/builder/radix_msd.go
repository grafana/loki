package builder

import (
	"encoding/binary"
	"fmt"
)

const msdInsertionMax = 32

// msdLSDSorter is "reverse sorting": a stable MSD scatter on a 16-bit digit
// (key bytes 0-1) splits the batch into buckets, and each bucket is finished
// by 8-bit LSD passes over the remaining bytes, which stay cache resident
// while the bucket holds at most budget pairs. Larger buckets take another
// 16-bit MSD level (bytes 2-3) first; buckets of at most 32 pairs use
// insertion sort. Constant digits are skipped at every level.
type msdLSDSorter struct {
	budget int
	hist16 [][1 << 16]int32 // one per MSD level
	hist8  [6][256]int32    // key bytes 5 down to 0

	msdScatters, lsdBuckets, insertionBuckets, copyBacks int
}

func newMSDLSDSorter(budget int) func() ngramSorter {
	return func() ngramSorter {
		return &msdLSDSorter{budget: budget, hist16: make([][1 << 16]int32, 3)}
	}
}

func (s *msdLSDSorter) stats() string {
	return fmt.Sprintf("msd_scatters=%d,lsd_buckets=%d,insertion_buckets=%d,copy_backs=%d",
		s.msdScatters, s.lsdBuckets, s.insertionBuckets, s.copyBacks)
}

func (s *msdLSDSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	if s.seg(keys, docs, kb[:n], db[:n], 0) {
		return kb, db
	}
	return keys, docs
}

// seg sorts the pairs in aK/aD by key bytes [from, 6), using bK/bD as scratch
// of the same length, and reports whether the result ended up in bK/bD.
func (s *msdLSDSorter) seg(aK [][8]byte, aD []uint32, bK [][8]byte, bD []uint32, from int) bool {
	n := len(aK)
	switch {
	case from >= 6 || n <= 1:
		return false
	case n <= msdInsertionMax:
		s.insertionBuckets++
		insertionSortByKey(aK, aD)
		return false
	case n <= s.budget || from >= 4:
		s.lsdBuckets++
		return s.lsd(aK, aD, bK, bD, from)
	}
	h := &s.hist16[from/2]
	clear(h[:])
	shift := uint(48 - 8*from)
	for i := range aK {
		h[uint16(binary.BigEndian.Uint64(aK[i][:])>>shift)]++
	}
	if int(h[uint16(binary.BigEndian.Uint64(aK[0][:])>>shift)]) == n {
		return s.seg(aK, aD, bK, bD, from+2)
	}
	s.msdScatters++
	exclusivePrefix(h[:], 0)
	aD = aD[:n]
	for i := range aK {
		v := uint16(binary.BigEndian.Uint64(aK[i][:]) >> shift)
		p := h[v]
		bK[p] = aK[i]
		bD[p] = aD[i]
		h[v] = p + 1
	}
	// h[v] now holds the end of bucket v; walk the buckets in order. The
	// histogram is per level, so the recursion below cannot clobber it.
	lo := int32(0)
	for v := range h {
		hi := h[v]
		if hi-lo > 1 && s.seg(bK[lo:hi], bD[lo:hi], aK[lo:hi], aD[lo:hi], from+2) {
			s.copyBacks++
			copy(bK[lo:hi], aK[lo:hi])
			copy(bD[lo:hi], aD[lo:hi])
		}
		lo = hi
	}
	return true
}

// lsd sorts aK/aD by key bytes [from, 6) with 8-bit LSD passes (byte 5
// first), skipping constant bytes, and reports whether the result is in bK/bD.
func (s *msdLSDSorter) lsd(aK [][8]byte, aD []uint32, bK [][8]byte, bD []uint32, from int) bool {
	n := len(aK)
	digits := 6 - from
	h := &s.hist8
	for d := range digits {
		clear(h[d][:])
	}
	switch digits {
	case 4:
		for i := range aK {
			k := &aK[i]
			h[0][k[5]]++
			h[1][k[4]]++
			h[2][k[3]]++
			h[3][k[2]]++
		}
	default:
		for i := range aK {
			k := &aK[i]
			for d := range digits {
				h[d][k[5-d]]++
			}
		}
	}
	fromK, toK := aK, bK
	fromD, toD := aD[:n], bD[:n]
	inB := false
	for d := range digits {
		byteIdx := 5 - d
		hd := &h[d]
		if int(hd[fromK[0][byteIdx]]) == n {
			continue
		}
		exclusivePrefix(hd[:], 0)
		for i := range fromK {
			v := fromK[i][byteIdx]
			p := hd[v]
			toK[p] = fromK[i]
			toD[p] = fromD[i]
			hd[v] = p + 1
		}
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
		inB = !inB
	}
	return inB
}

// insertionSortByKey orders a small batch by key bytes 0-5.
func insertionSortByKey(keys [][8]byte, docs []uint32) {
	docs = docs[:len(keys)]
	for i := 1; i < len(keys); i++ {
		k, d := keys[i], docs[i]
		kv := binary.BigEndian.Uint64(k[:])
		j := i
		for j > 0 && binary.BigEndian.Uint64(keys[j-1][:]) > kv {
			keys[j], docs[j] = keys[j-1], docs[j-1]
			j--
		}
		keys[j], docs[j] = k, d
	}
}
