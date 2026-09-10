package builder

import (
	"encoding/binary"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type pair struct {
	key [8]byte
	doc uint32
}

// randomPairs generates n pairs from a tiny alphabet so duplicate ngrams (and,
// with dupDocs, duplicate (ngram, docID) pairs) are frequent. Bytes 6-7 are
// zero, matching production 6-byte ngrams.
func randomPairs(t *testing.T, n int, dupDocs bool) []pair {
	t.Helper()
	rng := rand.New(rand.NewSource(int64(n) + 42))
	docSpace := uint32(1 << 20)
	if dupDocs {
		docSpace = 4 // heavy (ngram, docID) collisions
	}
	out := make([]pair, n)
	for i := range out {
		for b := range 6 {
			out[i].key[b] = byte('a' + rng.Intn(4))
		}
		out[i].doc = rng.Uint32() % docSpace
	}
	return out
}

func keyU64(k [8]byte) uint64 { return binary.BigEndian.Uint64(k[:]) }

// oracleSortDedupe is the reference implementation: comparison sort by
// (ngram, docID), then exact-pair dedupe.
func oracleSortDedupe(in []pair) []pair {
	s := append([]pair(nil), in...)
	sort.Slice(s, func(i, j int) bool {
		if keyU64(s[i].key) != keyU64(s[j].key) {
			return keyU64(s[i].key) < keyU64(s[j].key)
		}
		return s[i].doc < s[j].doc
	})
	out := make([]pair, 0, len(s))
	for i, p := range s {
		if i == 0 || p != s[i-1] {
			out = append(out, p)
		}
	}
	return out
}

func splitSoA(pairs []pair) ([][8]byte, []uint32) {
	keys := make([][8]byte, len(pairs))
	docs := make([]uint32, len(pairs))
	for i, p := range pairs {
		keys[i], docs[i] = p.key, p.doc
	}
	return keys, docs
}

func joinSoA(keys [][8]byte, docs []uint32) []pair {
	out := make([]pair, len(keys))
	for i := range keys {
		out[i] = pair{key: keys[i], doc: docs[i]}
	}
	return out
}

func TestRadixSortByNgram(t *testing.T) {
	hist := new([3][1 << 16]int32)
	pos := new([1 << 16]int32)

	for _, n := range []int{0, 1, 2, 100, 5000} {
		keys, docs := splitSoA(randomPairs(t, n, false))
		want := joinSoA(keys, docs) // multiset must be preserved

		kb := make([][8]byte, n)
		db := make([]uint32, n)
		sk, sd := radixSortByNgram(keys, docs, kb, db, hist, pos)
		require.Len(t, sk, n)
		require.Len(t, sd, n)

		// Keys must be fully ngram-sorted (docs unordered within a group).
		for i := 1; i < n; i++ {
			require.LessOrEqual(t, keyU64(sk[i-1]), keyU64(sk[i]), "keys out of order at %d (n=%d)", i, n)
		}

		// The (key, doc) multiset must be exactly the input's.
		got := oracleSortDedupeKeepDups(joinSoA(sk, sd))
		require.Equal(t, oracleSortDedupeKeepDups(want), got, "pair multiset changed (n=%d)", n)
	}
}

// oracleSortDedupeKeepDups canonicalizes a pair multiset for comparison
// (full sort, duplicates preserved).
func oracleSortDedupeKeepDups(in []pair) []pair {
	s := append([]pair(nil), in...)
	sort.Slice(s, func(i, j int) bool {
		if keyU64(s[i].key) != keyU64(s[j].key) {
			return keyU64(s[i].key) < keyU64(s[j].key)
		}
		return s[i].doc < s[j].doc
	})
	return s
}

func TestSortAndDedupeGroups(t *testing.T) {
	hist := new([3][1 << 16]int32)
	pos := new([1 << 16]int32)

	for _, tc := range []struct {
		name    string
		n       int
		dupDocs bool
	}{
		{"empty", 0, false},
		{"single", 1, false},
		{"varied", 3000, false},
		{"duplicate_heavy", 3000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairs := randomPairs(t, tc.n, tc.dupDocs)
			want := oracleSortDedupe(pairs)

			keys, docs := splitSoA(pairs)
			kb := make([][8]byte, tc.n)
			db := make([]uint32, tc.n)
			sk, sd := radixSortByNgram(keys, docs, kb, db, hist, pos)
			m := sortAndDedupeGroups(sk, sd)

			require.Equal(t, want, joinSoA(sk[:m], sd[:m]))
		})
	}
}

func TestMergeSortedPairs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		na, nb int
	}{
		{"both_empty", 0, 0},
		{"left_empty", 0, 200},
		{"right_empty", 200, 0},
		{"overlapping", 1000, 700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Duplicate-heavy inputs so the two runs share many exact pairs,
			// exercising the cross-run dedupe branch.
			a := oracleSortDedupe(randomPairs(t, tc.na, true))
			b := oracleSortDedupe(randomPairs(t, tc.nb, true))
			want := oracleSortDedupe(append(append([]pair(nil), a...), b...))

			ak, ad := splitSoA(a)
			bk, bd := splitSoA(b)
			ok := make([][8]byte, len(a)+len(b))
			od := make([]uint32, len(a)+len(b))
			w := mergeSortedPairs(ak, ad, bk, bd, ok, od)

			require.Equal(t, want, joinSoA(ok[:w], od[:w]))
		})
	}
}
