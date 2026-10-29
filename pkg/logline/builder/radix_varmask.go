package builder

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"unsafe"
)

const varmaskMaxRuns = 4

// maskRun moves (u >> shift) & mask to bit out of a digit.
type maskRun struct {
	shift, out uint
	mask       uint64
}

// maskDigit gathers some of a key's varying bits, in order, from up to
// varmaskMaxRuns runs of adjacent bits; unused runs have a zero mask.
type maskDigit struct {
	r     [varmaskMaxRuns]maskRun
	runs  int
	width uint
}

// Shift counts are masked with 63 so the compiler drops its oversized-shift
// handling; run shifts and offsets are below 64 by construction.

func (d *maskDigit) extract2(u uint64) uint64 {
	return u>>(d.r[0].shift&63)&d.r[0].mask | (u>>(d.r[1].shift&63)&d.r[1].mask)<<(d.r[1].out&63)
}

func (d *maskDigit) extract4(u uint64) uint64 {
	return u>>(d.r[0].shift&63)&d.r[0].mask | (u>>(d.r[1].shift&63)&d.r[1].mask)<<(d.r[1].out&63) |
		(u>>(d.r[2].shift&63)&d.r[2].mask)<<(d.r[2].out&63) | (u>>(d.r[3].shift&63)&d.r[3].mask)<<(d.r[3].out&63)
}

// varmaskSorter is LSD radix over only the key bits that vary within the
// batch: a scan ORs every key's XOR with the first key, and the varying bits
// are split into the fewest balanced digits of at most maxWidth bits. Constant
// bits cannot change the order, so dropping them keeps it. For uppercase text
// 7 bits vary per byte: 42 bits give 3 passes of 14 bits at maxWidth 16, or 2
// passes of 21 bits at maxWidth 21. Digits are gathered with shifts and masks
// over runs of adjacent bits (a portable PEXT); a digit that would need more
// than 4 runs makes the batch use the fallback sorter.
type varmaskSorter struct {
	maxWidth uint
	hist     [3][]int32
	digits   [3]maskDigit
	fallback ngramSorter

	sorts, fallbacks, passes, bitSum int
}

func newVarmaskSorter(maxWidth uint) func() ngramSorter {
	return func() ngramSorter {
		s := &varmaskSorter{maxWidth: maxWidth, fallback: newBaselineSorter()}
		for i := range s.hist {
			s.hist[i] = make([]int32, 1<<maxWidth)
		}
		return s
	}
}

func (s *varmaskSorter) stats() string {
	avg := 0.0
	if planned := s.sorts - s.fallbacks; planned > 0 {
		avg = float64(s.bitSum) / float64(planned)
	}
	return fmt.Sprintf("sorts=%d,fallbacks=%d,passes=%d,avg_varying_bits=%.1f", s.sorts, s.fallbacks, s.passes, avg)
}

// planDigits splits the set bits of vary, least significant first, into the
// fewest balanced digits of at most maxWidth bits, wider digits last.
func (s *varmaskSorter) planDigits(vary uint64) ([]maskDigit, bool) {
	w := uint(bits.OnesCount64(vary))
	p := (w + s.maxWidth - 1) / s.maxWidth
	base, extra := w/p, w%p
	digits := s.digits[:p]
	for i := range digits {
		d := &digits[i]
		*d = maskDigit{width: base}
		if uint(i) >= p-extra {
			d.width++
		}
		out := uint(0)
		for out < d.width {
			if d.runs == varmaskMaxRuns {
				return nil, false
			}
			lo := uint(bits.TrailingZeros64(vary))
			run := min(uint(bits.TrailingZeros64(^(vary >> lo))), d.width-out)
			d.r[d.runs] = maskRun{shift: lo, out: out, mask: 1<<run - 1}
			d.runs++
			vary &^= (1<<run - 1) << lo
			out += run
		}
	}
	return digits, true
}

func (s *varmaskSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	s.sorts++
	k0 := binary.BigEndian.Uint64(keys[0][:])
	var vary uint64
	for i := range keys {
		vary |= binary.BigEndian.Uint64(keys[i][:]) ^ k0
	}
	if vary == 0 {
		return keys, docs
	}
	digits, ok := s.planDigits(vary)
	if !ok {
		s.fallbacks++
		return s.fallback.sort(keys, docs, kb, db)
	}
	s.bitSum += bits.OnesCount64(vary)
	s.count(keys, digits)
	fromK, toK := keys, kb[:n]
	fromD, toD := docs, db[:n]
	for i := range digits {
		h := s.hist[i][:1<<digits[i].width]
		exclusivePrefix(h, 0)
		d := digits[i]
		r := &d.r
		switch {
		case d.runs <= 2:
			varmaskScatter2(fromK, fromD, toK, toD, h, r[0].shift, r[1].shift, r[1].out, r[0].mask, r[1].mask)
		case d.runs == 3:
			varmaskScatter3(fromK, fromD, toK, toD, h, r[0].shift, r[1].shift, r[1].out, r[2].shift, r[2].out, r[0].mask, r[1].mask, r[2].mask)
		default:
			varmaskScatter4(fromK, fromD, toK, toD, h, d)
		}
		s.passes++
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// The scatter and count kernels take the runs as scalars and are not inlined,
// so their loops keep the runs in registers. They index histograms through
// unsafe: a digit of width w is below 1<<w by construction.

//go:noinline
func varmaskScatter2(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, h []int32, s0, s1, o1 uint, m0, m1 uint64) {
	fromD = fromD[:len(fromK)]
	hp := unsafe.Pointer(unsafe.SliceData(h))
	for j := range fromK {
		u := binary.BigEndian.Uint64(fromK[j][:])
		pv := (*int32)(unsafe.Add(hp, (u>>(s0&63)&m0|(u>>(s1&63)&m1)<<(o1&63))*4))
		p := *pv
		toK[p] = fromK[j]
		toD[p] = fromD[j]
		*pv = p + 1
	}
}

//go:noinline
func varmaskScatter3(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, h []int32, s0, s1, o1, s2, o2 uint, m0, m1, m2 uint64) {
	fromD = fromD[:len(fromK)]
	hp := unsafe.Pointer(unsafe.SliceData(h))
	for j := range fromK {
		u := binary.BigEndian.Uint64(fromK[j][:])
		pv := (*int32)(unsafe.Add(hp, (u>>(s0&63)&m0|(u>>(s1&63)&m1)<<(o1&63)|(u>>(s2&63)&m2)<<(o2&63))*4))
		p := *pv
		toK[p] = fromK[j]
		toD[p] = fromD[j]
		*pv = p + 1
	}
}

//go:noinline
func varmaskScatter4(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, h []int32, d maskDigit) {
	fromD = fromD[:len(fromK)]
	hp := unsafe.Pointer(unsafe.SliceData(h))
	for j := range fromK {
		pv := (*int32)(unsafe.Add(hp, d.extract4(binary.BigEndian.Uint64(fromK[j][:]))*4))
		p := *pv
		toK[p] = fromK[j]
		toD[p] = fromD[j]
		*pv = p + 1
	}
}

// count builds every digit's histogram: three two-run digits (uppercase text
// at maxWidth 16) in one scan, anything else one scan per digit.
func (s *varmaskSorter) count(keys [][8]byte, digits []maskDigit) {
	for i := range digits {
		clear(s.hist[i][:1<<digits[i].width])
	}
	if len(digits) == 3 && digits[0].runs <= 2 && digits[1].runs <= 2 && digits[2].runs <= 2 {
		varmaskCount3x2(keys, s.hist[0], s.hist[1], s.hist[2], &digits[0].r, &digits[1].r, &digits[2].r)
		return
	}
	for i := range digits {
		d := digits[i]
		hp := unsafe.Pointer(unsafe.SliceData(s.hist[i]))
		if d.runs <= 3 {
			r := &d.r
			s0, s1, o1, s2, o2 := r[0].shift, r[1].shift, r[1].out, r[2].shift, r[2].out
			m0, m1, m2 := r[0].mask, r[1].mask, r[2].mask
			for j := range keys {
				u := binary.BigEndian.Uint64(keys[j][:])
				(*(*int32)(unsafe.Add(hp, (u>>(s0&63)&m0|(u>>(s1&63)&m1)<<(o1&63)|(u>>(s2&63)&m2)<<(o2&63))*4)))++
			}
			continue
		}
		for j := range keys {
			(*(*int32)(unsafe.Add(hp, d.extract4(binary.BigEndian.Uint64(keys[j][:]))*4)))++
		}
	}
}

//go:noinline
func varmaskCount3x2(keys [][8]byte, h0, h1, h2 []int32, d0, d1, d2 *[varmaskMaxRuns]maskRun) {
	a0, a1, ao, am0, am1 := d0[0].shift, d0[1].shift, d0[1].out, d0[0].mask, d0[1].mask
	b0, b1, bo, bm0, bm1 := d1[0].shift, d1[1].shift, d1[1].out, d1[0].mask, d1[1].mask
	c0, c1, co, cm0, cm1 := d2[0].shift, d2[1].shift, d2[1].out, d2[0].mask, d2[1].mask
	p0 := unsafe.Pointer(unsafe.SliceData(h0))
	p1 := unsafe.Pointer(unsafe.SliceData(h1))
	p2 := unsafe.Pointer(unsafe.SliceData(h2))
	for i := range keys {
		u := binary.BigEndian.Uint64(keys[i][:])
		(*(*int32)(unsafe.Add(p0, (u>>(a0&63)&am0|(u>>(a1&63)&am1)<<(ao&63))*4)))++
		(*(*int32)(unsafe.Add(p1, (u>>(b0&63)&bm0|(u>>(b1&63)&bm1)<<(bo&63))*4)))++
		(*(*int32)(unsafe.Add(p2, (u>>(c0&63)&cm0|(u>>(c1&63)&cm1)<<(co&63))*4)))++
	}
}
