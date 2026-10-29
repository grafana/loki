package builder

import (
	"encoding/binary"
	"unsafe"
)

// Shared LSD building blocks for the experimental ngram sorters. A key's sort
// value is key48: the 48 data bits of bytes 0-5, big-endian.

func key48(k *[8]byte) uint64 { return binary.BigEndian.Uint64(k[:]) >> 16 }

// lsdDigit selects (key48 >> shift) & mask.
type lsdDigit struct {
	shift uint
	mask  uint64
}

// balancedDigits splits the low `width` bits into the fewest digits of at most
// maxWidth bits, as equal in width as possible, least significant first.
func balancedDigits(width, maxWidth uint) []lsdDigit {
	if width == 0 {
		return nil
	}
	p := (width + maxWidth - 1) / maxWidth
	base, extra := width/p, width%p
	out := make([]lsdDigit, 0, p)
	shift := uint(0)
	for i := range p {
		w := base
		if i >= p-extra {
			w++
		}
		out = append(out, lsdDigit{shift: shift, mask: 1<<w - 1})
		shift += w
	}
	return out
}

func newDigitHists(digits []lsdDigit) [][]int32 {
	h := make([][]int32, len(digits))
	for i, d := range digits {
		h[i] = make([]int32, d.mask+1)
	}
	return h
}

// countDigits builds every digit's histogram over key48 in one scan. The
// common digit counts are unrolled so each element costs one load and one
// increment per digit.
func countDigits(keys [][8]byte, digits []lsdDigit, hists [][]int32) {
	switch len(digits) {
	case 3:
		s0, s1, s2 := digits[0].shift, digits[1].shift, digits[2].shift
		m0, m1, m2 := digits[0].mask, digits[1].mask, digits[2].mask
		h0, h1, h2 := hists[0], hists[1], hists[2]
		for i := range keys {
			k := key48(&keys[i])
			h0[(k>>s0)&m0]++
			h1[(k>>s1)&m1]++
			h2[(k>>s2)&m2]++
		}
	case 4:
		s0, s1, s2, s3 := digits[0].shift, digits[1].shift, digits[2].shift, digits[3].shift
		m0, m1, m2, m3 := digits[0].mask, digits[1].mask, digits[2].mask, digits[3].mask
		h0, h1, h2, h3 := hists[0], hists[1], hists[2], hists[3]
		for i := range keys {
			k := key48(&keys[i])
			h0[(k>>s0)&m0]++
			h1[(k>>s1)&m1]++
			h2[(k>>s2)&m2]++
			h3[(k>>s3)&m3]++
		}
	case 5:
		s0, s1, s2, s3, s4 := digits[0].shift, digits[1].shift, digits[2].shift, digits[3].shift, digits[4].shift
		m0, m1, m2, m3, m4 := digits[0].mask, digits[1].mask, digits[2].mask, digits[3].mask, digits[4].mask
		h0, h1, h2, h3, h4 := hists[0], hists[1], hists[2], hists[3], hists[4]
		for i := range keys {
			k := key48(&keys[i])
			h0[(k>>s0)&m0]++
			h1[(k>>s1)&m1]++
			h2[(k>>s2)&m2]++
			h3[(k>>s3)&m3]++
			h4[(k>>s4)&m4]++
		}
	case 6:
		h0, h1, h2, h3, h4, h5 := hists[0], hists[1], hists[2], hists[3], hists[4], hists[5]
		for i := range keys {
			k := &keys[i]
			h0[k[5]]++
			h1[k[4]]++
			h2[k[3]]++
			h3[k[2]]++
			h4[k[1]]++
			h5[k[0]]++
		}
	default:
		for i := range keys {
			k := key48(&keys[i])
			for d, dig := range digits {
				hists[d][(k>>dig.shift)&dig.mask]++
			}
		}
	}
}

// exclusivePrefix turns counts into bucket start offsets (plus base) in place.
func exclusivePrefix(h []int32, base int32) {
	sum := base
	for i, c := range h {
		h[i] = sum
		sum += c
	}
}

// scatterDigit is one stable LSD pass: element i goes to pos[digit]++.
func scatterDigit(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, dig lsdDigit, pos []int32) {
	fromD = fromD[:len(fromK)]
	for i := range fromK {
		v := (key48(&fromK[i]) >> dig.shift) & dig.mask
		p := pos[v]
		toK[p] = fromK[i]
		toD[p] = fromD[i]
		pos[v] = p + 1
	}
}

// lsdSorter is LSD radix over a fixed digit plan, with all histograms built in
// one up-front scan and constant digits skipped. With 16-bit digits it is the
// baseline algorithm; the width-N variants change only the plan.
type lsdSorter struct {
	digits []lsdDigit
	hists  [][]int32
}

func newLSDSorter(maxWidth uint) func() ngramSorter {
	return func() ngramSorter {
		d := balancedDigits(48, maxWidth)
		return &lsdSorter{digits: d, hists: newDigitHists(d)}
	}
}

func (s *lsdSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
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
		exclusivePrefix(h, 0)
		scatterDigit(fromK, fromD, toK, toD, dig, h)
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// fusedHistSorter is the baseline's 3 × 16-bit LSD, but instead of counting
// all three digits up front it counts only the first, and counts digit d+1
// while scattering digit d.
type fusedHistSorter struct {
	hist [3][1 << 16]int32
}

func newFusedHistSorter() ngramSorter { return &fusedHistSorter{} }

func (s *fusedHistSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	clear(s.hist[0][:])
	for i := range keys {
		s.hist[0][uint16(key48(&keys[i]))]++
	}
	fromK, toK := keys, kb[:n]
	fromD, toD := docs, db[:n]
	for d := range 3 {
		h := &s.hist[d]
		shift := uint(16 * d)
		var next *[1 << 16]int32
		if d < 2 {
			next = &s.hist[d+1]
			clear(next[:])
		}
		if int(h[uint16(key48(&fromK[0])>>shift)]) == n {
			if next != nil {
				for i := range fromK {
					next[uint16(key48(&fromK[i])>>(shift+16))]++
				}
			}
			continue
		}
		exclusivePrefix(h[:], 0)
		fd := fromD[:len(fromK)]
		if next != nil {
			for i := range fromK {
				k := key48(&fromK[i])
				v := uint16(k >> shift)
				next[uint16(k>>(shift+16))]++
				p := h[v]
				toK[p] = fromK[i]
				toD[p] = fd[i]
				h[v] = p + 1
			}
		} else {
			for i := range fromK {
				v := uint16(key48(&fromK[i]) >> shift)
				p := h[v]
				toK[p] = fromK[i]
				toD[p] = fd[i]
				h[v] = p + 1
			}
		}
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// tweakedSorter is the baseline's 3 × 16-bit LSD with inner-loop changes:
// hist2 counts alternating elements into two histogram copies (breaking the
// increment dependency chain on repeated digits), unroll scatters four
// elements per iteration through unsafe pointers (no bounds checks), and
// touchAhead > 0 loads the destination slot of element i+touchAhead before
// writing element i, emulating a software prefetch (Go has no intrinsic).
type tweakedSorter struct {
	hist       [3][1 << 16]int32
	hist2      *[3][1 << 16]int32
	unroll     bool
	touchAhead int
	sink       uint32
}

func newTweakedSorter(hist2, unroll bool, touchAhead int) func() ngramSorter {
	return func() ngramSorter {
		s := &tweakedSorter{unroll: unroll, touchAhead: touchAhead}
		if hist2 {
			s.hist2 = new([3][1 << 16]int32)
		}
		return s
	}
}

func (s *tweakedSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	for d := range s.hist {
		clear(s.hist[d][:])
	}
	if s.hist2 != nil {
		a, b := &s.hist, s.hist2
		for d := range b {
			clear(b[d][:])
		}
		i := 0
		for ; i+1 < n; i += 2 {
			k0 := key48(&keys[i])
			k1 := key48(&keys[i+1])
			a[0][uint16(k0)]++
			b[0][uint16(k1)]++
			a[1][uint16(k0>>16)]++
			b[1][uint16(k1>>16)]++
			a[2][uint16(k0>>32)]++
			b[2][uint16(k1>>32)]++
		}
		if i < n {
			k := key48(&keys[i])
			a[0][uint16(k)]++
			a[1][uint16(k>>16)]++
			a[2][uint16(k>>32)]++
		}
		for d := range a {
			for j := range a[d] {
				a[d][j] += b[d][j]
			}
		}
	} else {
		for i := range keys {
			k := key48(&keys[i])
			s.hist[0][uint16(k)]++
			s.hist[1][uint16(k>>16)]++
			s.hist[2][uint16(k>>32)]++
		}
	}

	fromK, toK := keys, kb[:n]
	fromD, toD := docs, db[:n]
	for d := range s.hist {
		h := &s.hist[d]
		shift := uint(16 * d)
		if int(h[uint16(key48(&fromK[0])>>shift)]) == n {
			continue
		}
		exclusivePrefix(h[:], 0)
		switch {
		case s.unroll:
			scatter16Unrolled(fromK, fromD, toK, toD, shift, h)
		case s.touchAhead > 0:
			s.sink ^= scatter16TouchAhead(fromK, fromD, toK, toD, shift, h, s.touchAhead)
		default:
			fd := fromD[:len(fromK)]
			for i := range fromK {
				v := uint16(key48(&fromK[i]) >> shift)
				p := h[v]
				toK[p] = fromK[i]
				toD[p] = fd[i]
				h[v] = p + 1
			}
		}
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// scatter16Unrolled is a 16-bit LSD pass, four elements per iteration. The
// loads and digit extraction of all four are issued before the dependent
// position updates, which stay in program order so equal digits stay stable.
// Destinations are in bounds because pos holds exclusive prefix sums of this
// pass's histogram over len(fromK) <= len(toK) elements.
func scatter16Unrolled(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, shift uint, pos *[1 << 16]int32) {
	n := len(fromK)
	if n == 0 {
		return
	}
	_ = fromD[n-1]
	_ = toK[n-1]
	_ = toD[n-1]
	fk := unsafe.Pointer(unsafe.SliceData(fromK))
	fd := unsafe.Pointer(unsafe.SliceData(fromD))
	tk := unsafe.Pointer(unsafe.SliceData(toK))
	td := unsafe.Pointer(unsafe.SliceData(toD))
	sh := shift + 16
	i := 0
	for ; i+4 <= n; i += 4 {
		k0 := *(*[8]byte)(unsafe.Add(fk, i*8))
		k1 := *(*[8]byte)(unsafe.Add(fk, i*8+8))
		k2 := *(*[8]byte)(unsafe.Add(fk, i*8+16))
		k3 := *(*[8]byte)(unsafe.Add(fk, i*8+24))
		d0 := *(*uint32)(unsafe.Add(fd, i*4))
		d1 := *(*uint32)(unsafe.Add(fd, i*4+4))
		d2 := *(*uint32)(unsafe.Add(fd, i*4+8))
		d3 := *(*uint32)(unsafe.Add(fd, i*4+12))
		v0 := uint16(binary.BigEndian.Uint64(k0[:]) >> sh)
		v1 := uint16(binary.BigEndian.Uint64(k1[:]) >> sh)
		v2 := uint16(binary.BigEndian.Uint64(k2[:]) >> sh)
		v3 := uint16(binary.BigEndian.Uint64(k3[:]) >> sh)
		p := uintptr(pos[v0])
		pos[v0] = int32(p) + 1
		*(*[8]byte)(unsafe.Add(tk, p*8)) = k0
		*(*uint32)(unsafe.Add(td, p*4)) = d0
		p = uintptr(pos[v1])
		pos[v1] = int32(p) + 1
		*(*[8]byte)(unsafe.Add(tk, p*8)) = k1
		*(*uint32)(unsafe.Add(td, p*4)) = d1
		p = uintptr(pos[v2])
		pos[v2] = int32(p) + 1
		*(*[8]byte)(unsafe.Add(tk, p*8)) = k2
		*(*uint32)(unsafe.Add(td, p*4)) = d2
		p = uintptr(pos[v3])
		pos[v3] = int32(p) + 1
		*(*[8]byte)(unsafe.Add(tk, p*8)) = k3
		*(*uint32)(unsafe.Add(td, p*4)) = d3
	}
	for ; i < n; i++ {
		k := *(*[8]byte)(unsafe.Add(fk, i*8))
		d := *(*uint32)(unsafe.Add(fd, i*4))
		v := uint16(binary.BigEndian.Uint64(k[:]) >> sh)
		p := uintptr(pos[v])
		pos[v] = int32(p) + 1
		*(*[8]byte)(unsafe.Add(tk, p*8)) = k
		*(*uint32)(unsafe.Add(td, p*4)) = d
	}
}

// scatter16TouchAhead is a 16-bit LSD pass that first reads the destination
// slots element i+dist will be written to, so their cache lines are already
// in flight when the write happens. The loaded bytes feed the returned value
// so the compiler cannot drop the loads. The slot is in bounds: element i+dist
// is not yet written, so its bucket's next position is below the bucket end.
func scatter16TouchAhead(fromK [][8]byte, fromD []uint32, toK [][8]byte, toD []uint32, shift uint, pos *[1 << 16]int32, dist int) uint32 {
	n := len(fromK)
	fd := fromD[:n]
	var sink uint32
	i := 0
	for ; i+dist < n; i++ {
		q := pos[uint16(key48(&fromK[i+dist])>>shift)]
		sink += uint32(toK[q][0]) + toD[q]
		v := uint16(key48(&fromK[i]) >> shift)
		p := pos[v]
		toK[p] = fromK[i]
		toD[p] = fd[i]
		pos[v] = p + 1
	}
	for ; i < n; i++ {
		v := uint16(key48(&fromK[i]) >> shift)
		p := pos[v]
		toK[p] = fromK[i]
		toD[p] = fd[i]
		pos[v] = p + 1
	}
	return sink
}
