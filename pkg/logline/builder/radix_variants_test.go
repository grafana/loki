package builder

import (
	"cmp"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// sorterInput is a generator of (key, doc) batches for the sorter property
// tests. Keys always have bytes 6-7 zero, matching production 6-byte ngrams.
type sorterInput struct {
	name string
	gen  func(rng *rand.Rand, n int) ([][8]byte, []uint32)
}

func genKeys(n int, docSpace uint32, rng *rand.Rand, keyFn func(i int, k *[8]byte)) ([][8]byte, []uint32) {
	keys := make([][8]byte, n)
	docs := make([]uint32, n)
	for i := range n {
		keyFn(i, &keys[i])
		if docSpace == 0 {
			docs[i] = rng.Uint32()
		} else {
			docs[i] = rng.Uint32() % docSpace
		}
	}
	return keys, docs
}

const textAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789. "

func sorterInputs() []sorterInput {
	in := []sorterInput{
		{"tiny_alphabet", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 1<<20, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = byte('a' + rng.Intn(4))
				}
			})
		}},
		{"full_48bit", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 0, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = byte(rng.Intn(256))
				}
			})
		}},
		{"constant_key", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 1<<20, rng, func(_ int, k *[8]byte) { copy(k[:], "ABCDEF") })
		}},
		{"zipf", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			dict := make([][8]byte, 1<<14)
			for i := range dict {
				for b := range 6 {
					dict[i][b] = textAlphabet[rng.Intn(len(textAlphabet))]
				}
			}
			z := rand.NewZipf(rng, 1.2, 1, uint64(len(dict)-1))
			return genKeys(n, 1<<16, rng, func(_ int, k *[8]byte) { *k = dict[z.Uint64()] })
		}},
		{"ascii_text", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 6_000_000, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = textAlphabet[rng.Intn(len(textAlphabet))]
				}
			})
		}},
		{"ascii_narrow_docs", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			keys, docs := genKeys(n, 1000, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = textAlphabet[rng.Intn(len(textAlphabet))]
				}
			})
			for i := range docs {
				docs[i] += 3_000_000_000
			}
			return keys, docs
		}},
		{"high_bit_bytes", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 1<<20, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = byte(0x80 + rng.Intn(128))
				}
			})
		}},
		{"doc_extremes", func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			keys, docs := genKeys(n, 0, rng, func(_ int, k *[8]byte) {
				for b := range 6 {
					k[b] = byte('a' + rng.Intn(3))
				}
			})
			for i := range docs {
				switch rng.Intn(3) {
				case 0:
					docs[i] = 0
				case 1:
					docs[i] = math.MaxUint32
				}
			}
			return keys, docs
		}},
	}
	for p := range 6 {
		in = append(in, sorterInput{fmt.Sprintf("single_varying_byte_%d", p), func(rng *rand.Rand, n int) ([][8]byte, []uint32) {
			return genKeys(n, 1<<20, rng, func(_ int, k *[8]byte) {
				copy(k[:], "QRSTUV")
				k[p] = byte(rng.Intn(256))
			})
		}})
	}
	return in
}

// canonicalPairs is the pair multiset in (key, doc) order, duplicates kept.
// keys must already be grouped by key in ascending order.
func canonicalPairs(keys [][8]byte, docs []uint32) []pair {
	out := joinSoA(keys, docs)
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && out[j].key == out[i].key {
			j++
		}
		slices.SortFunc(out[i:j], func(a, b pair) int { return cmp.Compare(a.doc, b.doc) })
		i = j
	}
	return out
}

// referenceSort orders a copy of the batch with the production sorter.
func referenceSort(keys [][8]byte, docs []uint32) ([][8]byte, []uint32) {
	k, d := slices.Clone(keys), slices.Clone(docs)
	sk, sd := newBaselineSorter().sort(k, d, make([][8]byte, len(k)), make([]uint32, len(d)))
	return sk, sd
}

const sorterGuard = 64

var (
	guardKey = [8]byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef}
	guardDoc = uint32(0xfeedface)
)

// guarded returns n-element windows into larger arrays whose surroundings hold
// sentinels, so writes outside [0, n) are detectable. The key and doc windows
// start kShift and dShift elements past the guard, to test sorters at
// arbitrary alignments.
func guarded(n, kShift, dShift int) ([][8]byte, []uint32, func(t *testing.T)) {
	kLead, dLead := sorterGuard+kShift, sorterGuard+dShift
	kAll := make([][8]byte, kLead+n+sorterGuard)
	dAll := make([]uint32, dLead+n+sorterGuard)
	for i := range kLead {
		kAll[i] = guardKey
	}
	for i := range dLead {
		dAll[i] = guardDoc
	}
	for i := range sorterGuard {
		kAll[kLead+n+i] = guardKey
		dAll[dLead+n+i] = guardDoc
	}
	check := func(t *testing.T) {
		t.Helper()
		for i := range kLead {
			require.Equal(t, guardKey, kAll[i], "write before key window")
		}
		for i := range dLead {
			require.Equal(t, guardDoc, dAll[i], "write before doc window")
		}
		for i := range sorterGuard {
			require.Equal(t, guardKey, kAll[kLead+n+i], "write after key window")
			require.Equal(t, guardDoc, dAll[dLead+n+i], "write after doc window")
		}
	}
	return kAll[kLead : kLead+n : kLead+n], dAll[dLead : dLead+n : dLead+n], check
}

// checkSorter sorts one batch with s and verifies every ngramSorter contract.
func checkSorter(t *testing.T, s ngramSorter, inKeys [][8]byte, inDocs []uint32, wantPairs, wantDedup []pair) {
	t.Helper()
	checkSorterAt(t, s, inKeys, inDocs, wantPairs, wantDedup, [4]int{})
}

// checkSorterAt is checkSorter with the keys, docs, key scratch and doc
// scratch windows shifted by the given element counts.
func checkSorterAt(t *testing.T, s ngramSorter, inKeys [][8]byte, inDocs []uint32, wantPairs, wantDedup []pair, shifts [4]int) {
	t.Helper()
	n := len(inKeys)
	keys, docs, checkA := guarded(n, shifts[0], shifts[1])
	kb, db, checkB := guarded(n, shifts[2], shifts[3])
	copy(keys, inKeys)
	copy(docs, inDocs)

	sk, sd := s.sort(keys, docs, kb, db)
	checkA(t)
	checkB(t)
	require.Len(t, sk, n)
	require.Len(t, sd, n)
	if n > 0 {
		inKeysSide := &sk[0] == &keys[0] && &sd[0] == &docs[0]
		inScratchSide := &sk[0] == &kb[0] && &sd[0] == &db[0]
		require.True(t, inKeysSide || inScratchSide, "result must be exactly keys/docs or kb/db")
	}
	for i := 1; i < n; i++ {
		require.LessOrEqual(t, keyU64(sk[i-1]), keyU64(sk[i]), "keys out of order at %d", i)
	}
	require.Equal(t, wantPairs, canonicalPairs(sk, sd), "pair multiset changed")
	m := sortAndDedupeGroups(sk, sd)
	require.Equal(t, wantDedup, joinSoA(sk[:m], sd[:m]))
}

func TestNgramSortVariants(t *testing.T) {
	sizes := []int{0, 1, 2, 3, 7, 100, 5000, 65537}
	if !testing.Short() {
		sizes = append(sizes, 1<<20)
	}
	inputs := sorterInputs()
	for _, v := range ngramSortVariants {
		t.Run(v.name, func(t *testing.T) {
			s := v.newSorter()
			for _, in := range inputs {
				for _, n := range sizes {
					if n == 1<<20 && in.name != "ascii_text" && in.name != "zipf" && in.name != "ascii_narrow_docs" && in.name != "full_48bit" {
						continue
					}
					rng := rand.New(rand.NewSource(int64(n)*31 + int64(len(in.name))))
					keys, docs := in.gen(rng, n)
					rk, rd := referenceSort(keys, docs)
					want := canonicalPairs(rk, rd)
					m := sortAndDedupeGroups(rk, rd)
					wantDedup := joinSoA(rk[:m], rd[:m])
					t.Run(fmt.Sprintf("%s/n=%d", in.name, n), func(t *testing.T) {
						checkSorter(t, s, keys, docs, want, wantDedup)
					})
				}
			}
		})
	}
}

// TestNgramSortVariantsReuse sorts batches of varying size and shape with one
// sorter instance, as a postings buffer does across fills, so state left over
// from a previous call cannot go unnoticed.
func TestNgramSortVariantsReuse(t *testing.T) {
	inputs := sorterInputs()
	plan := []struct {
		input string
		n     int
	}{
		{"full_48bit", 70000}, {"constant_key", 300}, {"ascii_text", 65537},
		{"tiny_alphabet", 3}, {"zipf", 120000}, {"ascii_narrow_docs", 9000},
		{"single_varying_byte_2", 40000}, {"full_48bit", 70000},
	}
	for _, v := range ngramSortVariants {
		t.Run(v.name, func(t *testing.T) {
			s := v.newSorter()
			for step, p := range plan {
				idx := slices.IndexFunc(inputs, func(in sorterInput) bool { return in.name == p.input })
				require.GreaterOrEqual(t, idx, 0)
				rng := rand.New(rand.NewSource(int64(step)))
				keys, docs := inputs[idx].gen(rng, p.n)
				rk, rd := referenceSort(keys, docs)
				want := canonicalPairs(rk, rd)
				m := sortAndDedupeGroups(rk, rd)
				checkSorter(t, s, keys, docs, want, joinSoA(rk[:m], rd[:m]))
			}
		})
	}
}

// TestNgramSortVariantsMisaligned sorts windows that start at different
// offsets within a cache line in each of the four arrays, so sorters that
// stage or copy whole lines must handle partial lines at every edge.
func TestNgramSortVariantsMisaligned(t *testing.T) {
	inputs := sorterInputs()
	shifts := [][4]int{{1, 1, 1, 1}, {3, 7, 5, 1}, {15, 2, 9, 14}, {0, 13, 8, 3}}
	for _, v := range ngramSortVariants {
		t.Run(v.name, func(t *testing.T) {
			s := v.newSorter()
			for _, name := range []string{"full_48bit", "zipf", "ascii_text"} {
				idx := slices.IndexFunc(inputs, func(in sorterInput) bool { return in.name == name })
				require.GreaterOrEqual(t, idx, 0)
				for _, n := range []int{37, 5000, 70001} {
					rng := rand.New(rand.NewSource(int64(n)))
					keys, docs := inputs[idx].gen(rng, n)
					rk, rd := referenceSort(keys, docs)
					want := canonicalPairs(rk, rd)
					m := sortAndDedupeGroups(rk, rd)
					wantDedup := joinSoA(rk[:m], rd[:m])
					for _, sh := range shifts {
						t.Run(fmt.Sprintf("%s/n=%d/shift=%v", name, n, sh), func(t *testing.T) {
							checkSorterAt(t, s, keys, docs, want, wantDedup, sh)
						})
					}
				}
			}
		})
	}
}

func TestNgramSortVariantRegistry(t *testing.T) {
	require.Equal(t, "baseline", ngramSortVariants[0].name, "the first variant is the production default")
	seen := map[string]bool{}
	for _, v := range ngramSortVariants {
		require.False(t, seen[v.name], "duplicate variant %q", v.name)
		seen[v.name] = true
		got, ok := lookupNgramSortVariant(v.name)
		require.True(t, ok)
		require.Equal(t, v.name, got.name)
	}
	_, ok := lookupNgramSortVariant("no-such-variant")
	require.False(t, ok)
}

// TestBlocksSorterSmallBlocks runs the block-recycling sorter with tiny digits
// and blocks, so block mode, multi-pass chains and compaction evictions all
// happen at sizes the registered configurations would hand to their fallback.
func TestBlocksSorterSmallBlocks(t *testing.T) {
	inputs := sorterInputs()
	for _, cfg := range []struct {
		width uint
		bs    int32
	}{{4, 1}, {4, 4}, {6, 3}, {8, 16}} {
		t.Run(fmt.Sprintf("w%d-b%d", cfg.width, cfg.bs), func(t *testing.T) {
			s := newBlocksSorter(cfg.width, cfg.bs)().(*blocksSorter)
			for _, in := range inputs {
				for _, n := range []int{2, 7, 100, 5000, 65537} {
					rng := rand.New(rand.NewSource(int64(n)*7 + int64(len(in.name))))
					keys, docs := in.gen(rng, n)
					rk, rd := referenceSort(keys, docs)
					want := canonicalPairs(rk, rd)
					m := sortAndDedupeGroups(rk, rd)
					wantDedup := joinSoA(rk[:m], rd[:m])
					t.Run(fmt.Sprintf("%s/n=%d", in.name, n), func(t *testing.T) {
						checkSorterAt(t, s, keys, docs, want, wantDedup, [4]int{0, 0, 3, 1})
					})
				}
			}
			t.Log(s.stats())
			require.Greater(t, s.passes, 0)
			require.Greater(t, s.evictions, 0)
			require.Less(t, s.fallbacks, s.sorts)
		})
	}
}
