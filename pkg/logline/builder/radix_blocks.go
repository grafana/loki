package builder

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

// blocksSorter is LSD radix with Radsort-style block recycling. Both arrays
// are cut into blocks of bs pairs: A (keys/docs) holds blocks [0, nA), B
// (kb/db) holds blocks [nA, nA+nB), and A's last n%bs pairs form an input-only
// tail block. A pass reads its input as a chain of blocks and appends each
// pair to its bucket's current block; when that fills, the bucket links a new
// block popped from a LIFO free list that every fully read input block is
// pushed to, so writes land in just-read, cache-warm memory and no histogram
// is needed. The bucket lists, concatenated in bucket order, are the next
// pass's input chain. A final compaction gathers the chain into A, first
// evicting any not-yet-copied block that sits where the output goes.
//
// A pass needs up to 2σ+2 partly filled blocks beyond the data itself, so
// inputs under bs·(2σ+4) pairs use the fallback sorter.
type blocksSorter struct {
	digits   []lsdDigit
	buckets  int
	bs       int32
	heads    []blockHead
	fallback ngramSorter

	// Block metadata, indexed by block id and reused across sorts.
	next, blen, free  []int32
	order, where      []int32
	freeA, freeB      []int32
	keys, kb          [][8]byte
	docs, db          []uint32
	nA, nB, tail      int32
	sorts, fallbacks  int
	passes, evictions int
}

// blockHead is a bucket's output state during a pass. fill == bs means the
// bucket needs a new block before its next pair.
type blockHead struct {
	k, d unsafe.Pointer
	fill int32
	blk  int32 // current (last) block, or -1
	head int32 // first block of the bucket this pass, or -1
}

func newBlocksSorter(maxWidth uint, blockSize int32) func() ngramSorter {
	return func() ngramSorter {
		d := balancedDigits(48, maxWidth)
		buckets := 0
		for _, dig := range d {
			buckets = max(buckets, int(dig.mask)+1)
		}
		return &blocksSorter{
			digits:   d,
			buckets:  buckets,
			bs:       blockSize,
			heads:    make([]blockHead, buckets),
			fallback: newBaselineSorter(),
		}
	}
}

func (s *blocksSorter) stats() string {
	return fmt.Sprintf("sorts=%d,fallbacks=%d,passes=%d,evictions=%d", s.sorts, s.fallbacks, s.passes, s.evictions)
}

func (s *blocksSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	s.sorts++
	if n < int(s.bs)*(2*s.buckets+4) {
		s.fallbacks++
		return s.fallback.sort(keys, docs, kb, db)
	}
	k0 := key48(&keys[0])
	var vary uint64
	for i := range keys {
		vary |= key48(&keys[i]) ^ k0
	}
	chain := int32(-1)
	for _, dig := range s.digits {
		if vary>>dig.shift&dig.mask == 0 {
			continue
		}
		if chain < 0 {
			chain = s.setup(keys, docs, kb[:n], db[:n])
		}
		chain = s.pass(chain, dig)
		s.passes++
	}
	if chain >= 0 {
		s.compact(chain)
		s.keys, s.docs, s.kb, s.db = nil, nil, nil, nil
	}
	return keys, docs
}

// setup lays out the blocks for one sort and returns A's blocks, in order, as
// the first pass's input chain. Every B block starts out free.
func (s *blocksSorter) setup(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) int32 {
	n, bs := int32(len(keys)), s.bs
	s.keys, s.docs, s.kb, s.db = keys, docs, kb, db
	s.nA, s.nB = n/bs, int32(len(kb))/bs
	s.tail = s.nA + s.nB
	ids := int(s.tail) + 1
	if cap(s.next) < ids {
		s.next = make([]int32, ids)
		s.blen = make([]int32, ids)
		s.where = make([]int32, ids)
		s.free = make([]int32, 0, ids)
		s.order = make([]int32, 0, ids)
		s.freeA = make([]int32, 0, ids)
		s.freeB = make([]int32, 0, ids)
	}
	s.next, s.blen, s.where = s.next[:ids], s.blen[:ids], s.where[:ids]
	for b := range s.nA {
		s.next[b] = b + 1
		s.blen[b] = bs
	}
	s.next[s.nA-1] = -1
	if rem := n % bs; rem > 0 {
		s.next[s.nA-1] = s.tail
		s.next[s.tail] = -1
		s.blen[s.tail] = rem
	}
	s.free = s.free[:0]
	for b := s.nA + s.nB - 1; b >= s.nA; b-- {
		s.free = append(s.free, b)
	}
	return 0
}

// blockPtrs returns the first key and doc of block b.
func (s *blocksSorter) blockPtrs(b int32) (unsafe.Pointer, unsafe.Pointer) {
	switch {
	case b < s.nA:
		i := int(b) * int(s.bs)
		return unsafe.Pointer(&s.keys[i]), unsafe.Pointer(&s.docs[i])
	case b < s.tail:
		i := int(b-s.nA) * int(s.bs)
		return unsafe.Pointer(&s.kb[i]), unsafe.Pointer(&s.db[i])
	default:
		i := int(s.nA) * int(s.bs)
		return unsafe.Pointer(&s.keys[i]), unsafe.Pointer(&s.docs[i])
	}
}

// pass distributes the chain starting at block chain by dig and returns the
// head of the resulting chain.
func (s *blocksSorter) pass(chain int32, dig lsdDigit) int32 {
	bs := s.bs
	heads := s.heads[:dig.mask+1]
	for v := range heads {
		heads[v] = blockHead{fill: bs, blk: -1, head: -1}
	}
	hp := unsafe.Pointer(unsafe.SliceData(heads))
	sh, mask := (dig.shift+16)&63, dig.mask
	for b := chain; b >= 0; {
		ln, nb := s.blen[b], s.next[b]
		kp, dp := s.blockPtrs(b)
		for j := range uintptr(ln) {
			src := unsafe.Add(kp, j*8)
			k := *(*uint64)(src)
			d := *(*uint32)(unsafe.Add(dp, j*4))
			v := uintptr(binary.BigEndian.Uint64((*[8]byte)(src)[:]) >> sh & mask)
			h := (*blockHead)(unsafe.Add(hp, v*unsafe.Sizeof(blockHead{})))
			if h.fill == bs {
				s.refill(h)
			}
			f := uintptr(h.fill)
			*(*uint64)(unsafe.Add(h.k, f*8)) = k
			*(*uint32)(unsafe.Add(h.d, f*4)) = d
			h.fill++
		}
		if b != s.tail {
			s.free = append(s.free, b)
		}
		b = nb
	}
	head, prev := int32(-1), int32(-1)
	for v := range heads {
		h := &heads[v]
		if h.blk < 0 {
			continue
		}
		s.blen[h.blk] = h.fill
		s.next[h.blk] = -1
		if prev < 0 {
			head = h.head
		} else {
			s.next[prev] = h.head
		}
		prev = h.blk
	}
	return head
}

// refill links a free block to the end of h's bucket.
//
//go:noinline
func (s *blocksSorter) refill(h *blockHead) {
	top := len(s.free) - 1
	q := s.free[top]
	s.free = s.free[:top]
	if h.blk >= 0 {
		s.next[h.blk] = q
		s.blen[h.blk] = s.bs
	} else {
		h.head = q
	}
	h.blk = q
	h.k, h.d = s.blockPtrs(q)
	h.fill = 0
}

// compact copies the sorted chain into A in order. Before a block is copied,
// any A block under its destination that still holds uncopied pairs is moved
// to a free B block, or to a free A block beyond the destination.
func (s *blocksSorter) compact(chain int32) {
	bs := int(s.bs)
	for i := range s.where {
		s.where[i] = -1
	}
	order := s.order[:0]
	for b := chain; b >= 0; b = s.next[b] {
		s.where[b] = int32(len(order))
		order = append(order, b)
	}
	s.freeA, s.freeB = s.freeA[:0], s.freeB[:0]
	for a := range s.nA {
		if s.where[a] < 0 {
			s.freeA = append(s.freeA, a)
		}
	}
	for b := s.tail - 1; b >= s.nA; b-- {
		if s.where[b] < 0 {
			s.freeB = append(s.freeB, b)
		}
	}
	f := 0
	for idx := range order {
		b := order[idx]
		ln := int(s.blen[b])
		s.where[b] = -1
		end := f + ln
		for a := f / bs; a < int(s.nA) && a*bs < end; a++ {
			w := s.where[a]
			if w < 0 {
				continue
			}
			q := s.evictTarget(end)
			s.copyBlock(q, int32(a), int(s.blen[a]))
			s.blen[q] = s.blen[a]
			order[w], s.where[q], s.where[a] = q, w, -1
			s.evictions++
		}
		kp, dp := s.blockPtrs(b)
		copy(s.keys[f:end], unsafe.Slice((*[8]byte)(kp), ln))
		copy(s.docs[f:end], unsafe.Slice((*uint32)(dp), ln))
		switch {
		case b >= s.nA && b < s.tail:
			s.freeB = append(s.freeB, b)
		case b < s.nA && int(b)*bs >= end:
			s.freeA = append(s.freeA, b)
		}
		f = end
	}
	s.order = order
}

// evictTarget pops a free block that the output, now written up to end,
// cannot reach yet.
func (s *blocksSorter) evictTarget(end int) int32 {
	if top := len(s.freeB) - 1; top >= 0 {
		q := s.freeB[top]
		s.freeB = s.freeB[:top]
		return q
	}
	for top := len(s.freeA) - 1; top >= 0; top-- {
		q := s.freeA[top]
		s.freeA = s.freeA[:top]
		if int(q)*int(s.bs) >= end {
			return q
		}
	}
	panic("blocksSorter: no free block to evict into")
}

func (s *blocksSorter) copyBlock(dst, src int32, ln int) {
	dk, dd := s.blockPtrs(dst)
	sk, sd := s.blockPtrs(src)
	copy(unsafe.Slice((*[8]byte)(dk), ln), unsafe.Slice((*[8]byte)(sk), ln))
	copy(unsafe.Slice((*uint32)(dd), ln), unsafe.Slice((*uint32)(sd), ln))
}
