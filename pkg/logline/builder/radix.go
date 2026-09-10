package builder

import (
	"encoding/binary"
	"slices"
)

// Pure sort/merge primitives over (ngram, docID) SoA pairs. These run on the
// critical path (every appended pair passes through them once per fill), so
// they are radix- and merge-based — never comparison sorts over the full batch.

// radixSortByNgram sorts keys/docs (tandem) by ngram only, using the 3
// data-bearing 16-bit ngram digits. Only key bytes 0-5 participate — bytes 6-7
// are assumed zero, which is why Validate caps ngram_length at 6: a 7- or
// 8-byte ngram would put data in the ignored digit and the output would be
// mis-sorted. docIDs are carried but not ordered within an ngram group.
// Returns whichever buffer holds the sorted result.
func radixSortByNgram(keys [][8]byte, docs []uint32, kb [][8]byte, db []uint32, hist *[3][1 << 16]int32, pos *[1 << 16]int32) ([][8]byte, []uint32) {
	n := len(keys)
	if n <= 1 {
		return keys, docs
	}
	// In the big-endian uint64 of the key, the low 16 bits (bytes 6-7) are
	// always zero, so the data-bearing digits sit at shifts 16/32/48 — hist
	// planes 0/1/2. LSD order over them sorts by the full 6 bytes.
	for d := range hist {
		for i := range hist[d] {
			hist[d][i] = 0
		}
	}
	for i := 0; i < n; i++ {
		k := binary.BigEndian.Uint64(keys[i][:])
		hist[0][uint16(k>>16)]++
		hist[1][uint16(k>>32)]++
		hist[2][uint16(k>>48)]++
	}
	fromK, toK := keys, kb[:n]
	fromD, toD := docs, db[:n]
	for d := range hist {
		h := &hist[d]
		shift := uint(16 * (d + 1))
		if int(h[uint16(binary.BigEndian.Uint64(fromK[0][:])>>shift)]) == n {
			continue // constant digit — identity permutation
		}
		pos[0] = 0
		for i := 1; i < (1 << 16); i++ {
			pos[i] = pos[i-1] + h[i-1]
		}
		for i := 0; i < n; i++ {
			v := uint16(binary.BigEndian.Uint64(fromK[i][:]) >> shift)
			p := pos[v]
			toK[p] = fromK[i]
			toD[p] = fromD[i]
			pos[v] = p + 1
		}
		fromK, toK = toK, fromK
		fromD, toD = toD, fromD
	}
	return fromK, fromD
}

// sortAndDedupeGroups takes an ngram-grouped SoA (keys equal within a group,
// docs unordered) and produces the fully sorted+deduped result in place:
// within each equal-ngram run it sorts the docID sub-slice and drops adjacent
// duplicates. Returns the compacted length.
func sortAndDedupeGroups(keys [][8]byte, docs []uint32) int {
	n := len(keys)
	w := 0
	i := 0
	for i < n {
		j := i + 1
		for j < n && keys[j] == keys[i] {
			j++
		}
		slices.Sort(docs[i:j]) // adaptive: near-sorted groups are ~O(group)
		term := keys[i]
		prev := uint32(0)
		first := true
		for m := i; m < j; m++ {
			if first || docs[m] != prev {
				keys[w] = term
				docs[w] = docs[m]
				prev = docs[m]
				w++
				first = false
			}
		}
		i = j
	}
	return w
}

// mergeSortedPairs 2-way-merges two (ngram, docID)-sorted+deduped SoA runs into
// ok/od, dropping pairs equal in both ngram and docID. Returns the merged
// length. ok/od must have cap >= len(ak)+len(bk).
func mergeSortedPairs(ak [][8]byte, ad []uint32, bk [][8]byte, bd []uint32, ok [][8]byte, od []uint32) int {
	i, j, w := 0, 0, 0
	na, nb := len(ak), len(bk)
	for i < na && j < nb {
		au := binary.BigEndian.Uint64(ak[i][:])
		bu := binary.BigEndian.Uint64(bk[j][:])
		if au < bu || (au == bu && ad[i] < bd[j]) {
			ok[w], od[w] = ak[i], ad[i]
			i++
		} else if au > bu || (au == bu && ad[i] > bd[j]) {
			ok[w], od[w] = bk[j], bd[j]
			j++
		} else { // equal (ngram, docID) — emit once, advance both
			ok[w], od[w] = ak[i], ad[i]
			i++
			j++
		}
		w++
	}
	for ; i < na; i++ {
		ok[w], od[w] = ak[i], ad[i]
		w++
	}
	for ; j < nb; j++ {
		ok[w], od[w] = bk[j], bd[j]
		w++
	}
	return w
}
