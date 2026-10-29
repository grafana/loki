package builder

import (
	"encoding/binary"
	"unsafe"
)

const swwcSlots = 16

type (
	swwcLineK [swwcSlots]uint64 // raw key words: a local [8]byte would live on the stack
	swwcLineD [swwcSlots]uint32
)

// swwcLine is one bucket's staging area: its next 16 keys (two cache lines)
// and their docs (one cache line), kept together so the scatter loop needs a
// single base pointer.
type swwcLine struct {
	k swwcLineK
	d swwcLineD
}

// swwcSorter is LSD radix with software write-combining (Wassenberg and
// Sanders): every bucket stages its next 16 pairs in a swwcLine, and a full
// line goes to its destination as one block copy per array, with non-temporal
// stores when nt is set. A slot is the destination index's position within its
// 128-byte key line, so with line-aligned buffers every full flush covers whole
// cache lines; partial lines at bucket edges are copied normally.
type swwcSorter struct {
	digits []lsdDigit
	hists  [][]int32
	start  []int32
	lines  []swwcLine
	nt     bool

	// The pass in progress: its destination, and the line offset of toK[0]
	// that pos and start values carry during the pass.
	toK [][8]byte
	toD []uint32
	off int32
}

func newSWWCSorter(maxWidth uint, nt bool) func() ngramSorter {
	return func() ngramSorter {
		d := balancedDigits(48, maxWidth)
		buckets := 0
		for _, dig := range d {
			buckets = max(buckets, int(dig.mask)+1)
		}
		return &swwcSorter{
			digits: d,
			hists:  newDigitHists(d),
			start:  make([]int32, buckets),
			lines:  make([]swwcLine, buckets),
			nt:     nt,
		}
	}
}

func (s *swwcSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	for _, h := range s.hists {
		clear(h)
	}
	countDigits(keys, s.digits, s.hists)
	fromK, toK := keys, kb[:n]
	fromD, toD := docs, db[:n]
	for d, dig := range s.digits {
		h := s.hists[d]
		if int(h[(key48(&fromK[0])>>dig.shift)&dig.mask]) == n {
			continue
		}
		s.scatter(fromK, fromD, toK, toD, dig, h)
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// scatter is one stable LSD pass through the staging lines; hist is the pass's
// histogram. Positions are kept offset by toK[0]'s slot so the slot of a
// position is its low 4 bits. The inner loop stages only and breaks out to
// flush, which keeps its state in registers.
func (s *swwcSorter) scatter(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, dig lsdDigit, hist []int32) {
	n := len(fromK)
	fromD = fromD[:n]
	s.toK, s.toD = toK[:n], toD[:n]
	s.off = int32(uintptr(unsafe.Pointer(&toK[0])) / 8 % swwcSlots)
	exclusivePrefix(hist, s.off)
	pos := hist
	copy(s.start[:len(pos)], pos)
	bl := unsafe.Pointer(unsafe.SliceData(s.lines))
	pp := unsafe.Pointer(unsafe.SliceData(pos))
	fk := unsafe.Pointer(unsafe.SliceData(fromK))
	sh, mask := (dig.shift+16)&63, dig.mask
	for i := 0; i < n; {
		full, fullV, fullP := false, uintptr(0), int32(0)
		for i < n {
			src := (*[8]byte)(unsafe.Add(fk, i*8))
			k := *(*uint64)(unsafe.Pointer(src))
			v := uintptr(binary.BigEndian.Uint64(src[:]) >> sh & mask)
			pv := (*int32)(unsafe.Add(pp, v*4))
			p := *pv
			*pv = p + 1
			slot := uintptr(p) % swwcSlots
			l := (*swwcLine)(unsafe.Add(bl, v*unsafe.Sizeof(swwcLine{})))
			l.k[slot] = k
			l.d[slot] = fromD[i]
			i++
			if slot == swwcSlots-1 {
				full, fullV, fullP = true, v, p
				break
			}
		}
		if full {
			s.flushLine(int(fullV), fullP)
		}
	}
	s.drain(pos)
}

// drain writes every bucket's partly filled last line once the pass's input is
// exhausted, then ends the pass.
func (s *swwcSorter) drain(pos []int32) {
	start := s.start[:len(pos)]
	toK := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.SliceData(s.toK))), len(s.toK))
	for v, p := range pos {
		r := p % swwcSlots
		if r == 0 {
			continue
		}
		ls := p - r
		b := max(ls, start[v])
		copy(toK[b-s.off:p-s.off], s.lines[v].k[b-ls:r])
		copy(s.toD[b-s.off:p-s.off], s.lines[v].d[b-ls:r])
	}
	if s.nt {
		sfence()
	}
	s.toK, s.toD = nil, nil
}

// flushLine writes bucket v's full staging line, whose last pair went to
// position p, to positions p-15 through p, or only from the bucket's start
// when the bucket begins inside that line.
//
//go:noinline
func (s *swwcSorter) flushLine(v int, p int32) {
	l := &s.lines[v]
	ls := p - (swwcSlots - 1)
	toK := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.SliceData(s.toK))), len(s.toK))
	if b := s.start[v]; ls < b {
		copy(toK[b-s.off:p+1-s.off], l.k[b-ls:])
		copy(s.toD[b-s.off:p+1-s.off], l.d[b-ls:])
		return
	}
	dk := (*swwcLineK)(toK[ls-s.off : p+1-s.off])
	dd := (*swwcLineD)(s.toD[ls-s.off : p+1-s.off])
	if s.nt {
		ntFlushLine(unsafe.Pointer(dk), unsafe.Pointer(&l.k), unsafe.Pointer(dd), unsafe.Pointer(&l.d))
	} else {
		*dk = l.k
		*dd = l.d
	}
}
