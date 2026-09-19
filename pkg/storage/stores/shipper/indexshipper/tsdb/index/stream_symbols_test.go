package index

import (
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
