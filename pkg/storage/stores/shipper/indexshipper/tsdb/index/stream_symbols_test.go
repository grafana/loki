package index

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

// writeManySymbolsFixture builds an index with well over symbolFactor
// symbols so the sparse offset table has multiple entries and a lookup must
// walk forward within a sparse block. Each series gets a unique zero-padded
// "id" value; symbols are the sorted union of the label name and every value.
func writeManySymbolsFixture(t *testing.T, format int) string {
	t.Helper()

	const numSeries = symbolFactor*4 + 5 // 133: several full sparse blocks plus a remainder.

	series := make([]seriesFixture, 0, numSeries)
	for i := range numSeries {
		series = append(series, seriesFixture{
			ls:     labels.FromStrings("id", fmt.Sprintf("v%04d", i)),
			chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}},
		})
	}

	return writeIndexFixture(t, format, series)
}

// TestStreamSymbols_LookupMatchesMmap asserts that streamSymbols.Lookup and
// Symbols.Lookup have matching behavior.
func TestStreamSymbols_LookupMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			mmap, stream := openBothReaders(t, writeManySymbolsFixture(t, format))

			// Both symbol tables must agree on how many symbols exist.
			require.Equal(t, mmap.symbols.seen, stream.symbols.size)
			count := stream.symbols.size

			// Every in-range ordinal resolves to the same symbol in both readers.
			for i := range count {
				mmapSymbol, err := mmap.symbols.Lookup(uint32(i))
				require.NoError(t, err)
				streamSymbol, err := stream.symbols.Lookup(uint32(i))
				require.NoError(t, err)
				require.Equal(t, mmapSymbol, streamSymbol)
			}

			// Out-of-range ordinals are rejected identically by both.
			for _, n := range []uint32{uint32(count), uint32(count) + 1, uint32(count) + 100} {
				_, mmapErr := mmap.symbols.Lookup(n)
				_, streamErr := stream.symbols.Lookup(n)
				require.Error(t, mmapErr)
				require.Error(t, streamErr)
				require.Equal(t, mmapErr.Error(), streamErr.Error())
			}
		})
	}
}

// writeWideSymbolsFixture builds an index with more symbols than a reader caches.
func writeWideSymbolsFixture(t *testing.T, format int) (string, int64) {
	t.Helper()

	// Two unique values per series, so the symbol table exceeds
	// labelValueSymbolsCacheSize without writing that many series.
	const numSeries = labelValueSymbolsCacheSize*2 + 100

	series := make([]seriesFixture, 0, numSeries)
	for i := range numSeries {
		series = append(series, seriesFixture{
			ls: labels.FromStrings(
				"id", fmt.Sprintf("series%05d", i),
				"pod", fmt.Sprintf("pod%05d", i),
				"app", fmt.Sprintf("app%02d", i%7),
				"env", []string{"dev", "prod", "staging"}[i%3],
				"tenant", "loki",
			),
			chunks: []ChunkMeta{{
				Checksum: uint32(i),
				MinTime:  0,
				MaxTime:  chunkSpan - 1,
				KB:       1,
				Entries:  1,
			}},
		})
	}

	return writeIndexFixture(t, format, series), chunkSpan
}

// TestStreamSymbols_LookupMatchesMmapPastCacheEviction runs the cross-check
// against the mmap reader over an index with more symbols than the cache holds,
// so lookups evict each other while it runs.
func TestStreamSymbols_LookupMatchesMmapPastCacheEviction(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, _ := writeWideSymbolsFixture(t, format)
			mmap, stream := openBothReaders(t, path)

			require.Equal(t, mmap.symbols.seen, stream.symbols.size)
			// Big enough we'll certainly be exercising plenty of eviction
			require.Greater(t, stream.symbols.size, labelValueSymbolsCacheSize*4)

			ordinals := make([]uint32, stream.symbols.size)
			for i := range ordinals {
				ordinals[i] = uint32(i)
			}
			rnd := rand.New(rand.NewSource(1))
			rnd.Shuffle(len(ordinals), func(i, j int) { ordinals[i], ordinals[j] = ordinals[j], ordinals[i] })

			for _, n := range ordinals {
				mmapResolvedSymbol, err := mmap.symbols.Lookup(n)
				require.NoError(t, err)
				streamResolvedSymbol, err := stream.symbols.Lookup(n)
				require.NoError(t, err)
				require.Equal(t, mmapResolvedSymbol, streamResolvedSymbol)
			}
		})
	}
}

// writeSymbolCountFixture builds an index whose symbol table holds exactly
// numSymbols symbols: the label name "id" and numSymbols-1 distinct values.
func writeSymbolCountFixture(t testing.TB, format, numSymbols int) string {
	t.Helper()

	series := make([]seriesFixture, 0, numSymbols-1)
	for i := range numSymbols - 1 {
		series = append(series, seriesFixture{
			ls:     labels.FromStrings("id", fmt.Sprintf("v%05d", i)),
			chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}},
		})
	}
	return writeIndexFixture(t, format, series)
}

// labelValueOrdinals returns up to limit ordinals in s that are not label names, lowest first.
func labelValueOrdinals(s *streamSymbols, limit int) []uint32 {
	var ordinals []uint32
	for i := 0; i < s.size && len(ordinals) < limit; i++ {
		if _, ok := s.labelNameSymbols[uint32(i)]; !ok {
			ordinals = append(ordinals, uint32(i))
		}
	}
	return ordinals
}

// BenchmarkStreamSymbols measures, by the number of symbols in an index:
//   - new: building a reader's streamSymbols. B/op is what each open index
//     file holds for its symbols, including the label value cache if it is
//     allocated up front.
//   - new_lookup: building a streamSymbols and looking up one label value, so
//     the cache is allocated.
//   - hit: label value lookups served from the cache.
//   - miss: lookups of two ordinals that share a cache entry, so each one
//     reads the file. Only an index with more symbols than the cache has them.
func BenchmarkStreamSymbols(b *testing.B) {
	for _, numSymbols := range []int{10, 100, 1000, 1024, 10000} {
		b.Run(fmt.Sprintf("symbols=%d", numSymbols), func(b *testing.B) {
			r, err := NewStreamFileReader(writeSymbolCountFixture(b, FormatV4, numSymbols), DefaultStreamOptions())
			require.NoError(b, err)
			b.Cleanup(func() { require.NoError(b, r.Close()) })
			symbols := r.symbols
			first := labelValueOrdinals(symbols, 1)[0]

			for _, lookup := range []bool{false, true} {
				name := "new"
				if lookup {
					name = "new_lookup"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						s, err := newStreamSymbols(context.Background(), symbols.factory, symbols.off, r.postings.isLabelName)
						if err != nil {
							b.Fatal(err)
						}
						if lookup {
							if _, err := s.Lookup(first); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}

			b.Run("hit", func(b *testing.B) {
				// Few enough ordinals that each has its own cache entry.
				ordinals := labelValueOrdinals(symbols, 64)
				for _, n := range ordinals {
					_, err := symbols.Lookup(n)
					require.NoError(b, err)
				}
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					if _, err := symbols.Lookup(ordinals[i%len(ordinals)]); err != nil {
						b.Fatal(err)
					}
					i++
				}
			})

			colliding := first + labelValueSymbolsCacheSize
			if int(colliding) >= symbols.size {
				return
			}
			b.Run("miss", func(b *testing.B) {
				b.ReportAllocs()
				ordinals := [2]uint32{first, colliding}
				i := 0
				for b.Loop() {
					if _, err := symbols.Lookup(ordinals[i&1]); err != nil {
						b.Fatal(err)
					}
					i++
				}
			})
		})
	}
}
