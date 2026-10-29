package builder

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"unsafe"
)

// packedSorter moves less data per pass. Each key byte position is replaced
// by its rank among the byte values present at that position in the batch,
// which keeps the order and needs only bits.Len(distinct-1) bits per position
// (6 for uppercase text). When the rank key plus the doc's offset from the
// batch's smallest doc fit in 64 bits, every pair is packed into one uint64,
// rank key on top, and LSD passes over the rank-key bits move 8-byte words in
// one stream instead of 12 bytes in two. Packed words ping-pong between the
// scratch and key arrays, both viewed as []uint64, and the last pass unpacks
// each word back to key and doc. Batches that do not fit use the fallback.
//
// Packing ORs six lookups in tables of pre-shifted ranks, and unpacking
// decodes adjacent position pairs through byte-pair tables, so neither needs
// a variable shift per position.
type packedSorter struct {
	hist     [3][1 << 16]int32
	seen     [6][256]bool
	rank     [6][256]uint8
	unrank   [6][256]uint8
	count    [6]int
	off      [6]uint // bit offset of each position's rank in the rank key
	width    [6]uint
	packT    [6][256]uint64
	pairT    [3][]uint16
	fallback ngramSorter

	sorts, packed, fallbacks, keyBitSum, docBitSum int
}

func newPackedSorter() ngramSorter {
	return &packedSorter{fallback: newBaselineSorter()}
}

func (s *packedSorter) stats() string {
	avgK, avgD := 0.0, 0.0
	if s.packed > 0 {
		avgK = float64(s.keyBitSum) / float64(s.packed)
		avgD = float64(s.docBitSum) / float64(s.packed)
	}
	return fmt.Sprintf("sorts=%d,packed=%d,fallbacks=%d,avg_key_bits=%.1f,avg_doc_bits=%.1f",
		s.sorts, s.packed, s.fallbacks, avgK, avgD)
}

func (s *packedSorter) sort(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	s.sorts++
	docs = docs[:n]
	minDoc, maxDoc := s.scan(keys, docs)
	keyBits := s.buildRanks()
	if keyBits == 0 {
		return keys, docs
	}
	docBits := uint(bits.Len32(maxDoc - minDoc))
	if keyBits+docBits > 64 {
		s.fallbacks++
		return s.fallback.sort(keys, docs, kb, db)
	}
	s.packed++
	s.keyBitSum += int(keyBits)
	s.docBitSum += int(docBits)
	s.buildTables(docBits)

	digits := balancedDigits(keyBits, 16)
	for d := range digits {
		digits[d].shift += docBits
	}
	kw := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.SliceData(kb))), n)
	aw := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.SliceData(keys))), n)
	s.pack(keys, docs, kw, minDoc, digits)

	from, to := kw, aw
	for d := range len(digits) - 1 {
		h := &s.hist[d]
		exclusivePrefix(h[:digits[d].mask+1], 0)
		packedScatter(from, to, h, digits[d].shift, digits[d].mask)
		from, to = to, from
	}
	outK, outD := keys, docs
	if &from[0] == &aw[0] {
		outK, outD = kb[:n], db[:n]
	}
	last := digits[len(digits)-1]
	h := &s.hist[len(digits)-1]
	exclusivePrefix(h[:last.mask+1], 0)
	s.unpack(from, outK, outD, h, last, minDoc, docBits)
	if &outK[0] == &keys[0] {
		return keys, docs
	}
	return kb, db
}

// scan records which byte values occur at each key position, and returns the
// smallest and largest doc.
func (s *packedSorter) scan(keys [][8]byte, docs []uint32) (uint32, uint32) {
	s.seen = [6][256]bool{}
	sn := &s.seen
	minDoc, maxDoc := docs[0], docs[0]
	docs = docs[:len(keys)]
	for i := range keys {
		k := &keys[i]
		sn[0][k[0]] = true
		sn[1][k[1]] = true
		sn[2][k[2]] = true
		sn[3][k[3]] = true
		sn[4][k[4]] = true
		sn[5][k[5]] = true
		minDoc = min(minDoc, docs[i])
		maxDoc = max(maxDoc, docs[i])
	}
	return minDoc, maxDoc
}

// buildRanks fills the rank and unrank tables from the seen sets and lays the
// positions out in the rank key, position 5 least significant. It returns the
// rank key's width.
func (s *packedSorter) buildRanks() uint {
	keyBits := uint(0)
	for p := 5; p >= 0; p-- {
		r := 0
		for v := range 256 {
			if s.seen[p][v] {
				s.rank[p][v] = uint8(r)
				s.unrank[p][r] = uint8(v)
				r++
			}
		}
		w := uint(bits.Len(uint(r - 1)))
		s.count[p], s.off[p], s.width[p] = r, keyBits, w
		keyBits += w
	}
	return keyBits
}

// buildTables fills the pre-shifted rank tables used by pack and the
// byte-pair tables used by unpack. Pair table j maps the ranks of positions
// 2j and 2j+1, as laid out in the rank key, to those two key bytes.
func (s *packedSorter) buildTables(docBits uint) {
	for p := range 6 {
		sh := (s.off[p] + docBits) & 63
		for v := range 256 {
			if s.seen[p][v] {
				s.packT[p][v] = uint64(s.rank[p][v]) << sh
			}
		}
	}
	for j := range 3 {
		hi, lo := 2*j, 2*j+1
		size := 1 << (s.width[hi] + s.width[lo])
		if cap(s.pairT[j]) < size {
			s.pairT[j] = make([]uint16, size)
		}
		t := s.pairT[j][:size]
		for rh := range s.count[hi] {
			for rl := range s.count[lo] {
				t[rh<<s.width[lo]|rl] = uint16(s.unrank[hi][rh])<<8 | uint16(s.unrank[lo][rl])
			}
		}
		s.pairT[j] = t
	}
}

// pack writes each pair's packed word to kw and counts every digit. A rank
// key has at most 48 bits, so at most 3 digits; missing ones have a zero
// mask and count into spare histograms that are never read.
func (s *packedSorter) pack(keys [][8]byte, docs []uint32, kw []uint64, minDoc uint32, digits []lsdDigit) {
	for d := range digits {
		clear(s.hist[d][:digits[d].mask+1])
	}
	var sh [3]uint
	var mask [3]uint64
	for d := range digits {
		sh[d], mask[d] = digits[d].shift&63, digits[d].mask
	}
	packWords(keys, docs, kw, &s.packT, minDoc, &s.hist, sh[0], sh[1], sh[2], mask[0], mask[1], mask[2])
}

//go:noinline
func packWords(keys [][8]byte, docs []uint32, kw []uint64, t *[6][256]uint64, minDoc uint32, h *[3][1 << 16]int32, s0, s1, s2 uint, m0, m1, m2 uint64) {
	kw = kw[:len(keys)]
	docs = docs[:len(keys)]
	h0, h1, h2 := &h[0], &h[1], &h[2]
	for i := range keys {
		k := &keys[i]
		w := t[0][k[0]] | t[1][k[1]] | t[2][k[2]] | t[3][k[3]] | t[4][k[4]] | t[5][k[5]] | uint64(docs[i]-minDoc)
		kw[i] = w
		h0[uint16(w>>(s0&63)&m0)]++
		h1[uint16(w>>(s1&63)&m1)]++
		h2[uint16(w>>(s2&63)&m2)]++
	}
}

// packedScatter is one LSD pass over packed words.
//
//go:noinline
func packedScatter(from, to []uint64, h *[1 << 16]int32, shift uint, mask uint64) {
	shift &= 63
	for _, w := range from {
		v := uint16(w >> shift & mask)
		p := h[v]
		to[p] = w
		h[v] = p + 1
	}
}

// unpack is the last LSD pass: it scatters by the top digit and writes each
// word back as a key and doc.
func (s *packedSorter) unpack(from []uint64, outK [][8]byte, outD []uint32, h *[1 << 16]int32, dig lsdDigit, minDoc uint32, docBits uint) {
	var sh [3]uint
	var mask [3]uint64
	for j := range 3 {
		lo := 2*j + 1
		sh[j] = (s.off[lo] + docBits) & 63
		mask[j] = 1<<(s.width[2*j]+s.width[lo]) - 1
	}
	unpackWords(from, outK, outD, h, dig.shift, dig.mask, minDoc, uint64(1)<<(docBits&63)-1,
		s.pairT[0], s.pairT[1], s.pairT[2], sh[0], sh[1], sh[2], mask[0], mask[1], mask[2])
}

//go:noinline
func unpackWords(from []uint64, outK [][8]byte, outD []uint32, h *[1 << 16]int32, shift uint, mask uint64, minDoc uint32, docMask uint64,
	t0, t1, t2 []uint16, s0, s1, s2 uint, m0, m1, m2 uint64) {
	p0 := unsafe.Pointer(unsafe.SliceData(t0))
	p1 := unsafe.Pointer(unsafe.SliceData(t1))
	p2 := unsafe.Pointer(unsafe.SliceData(t2))
	shift &= 63
	for _, w := range from {
		v := uint16(w >> shift & mask)
		p := h[v]
		h[v] = p + 1
		// The pair indexes are below each table's length by construction.
		b01 := *(*uint16)(unsafe.Add(p0, (w>>(s0&63)&m0)*2))
		b23 := *(*uint16)(unsafe.Add(p1, (w>>(s1&63)&m1)*2))
		b45 := *(*uint16)(unsafe.Add(p2, (w>>(s2&63)&m2)*2))
		binary.BigEndian.PutUint64(outK[p][:], uint64(b01)<<48|uint64(b23)<<32|uint64(b45)<<16)
		outD[p] = uint32(w&docMask) + minDoc
	}
}
