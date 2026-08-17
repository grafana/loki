package index

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// writeManySymbolsFixture builds an index with well over symbolFactor
// symbols so the sparse offset table has multiple entries and a lookup must
// walk forward within a sparse block. Each series gets a unique zero-padded
// "id" value; symbols are the sorted union of the label name and every value.
func writeManySymbolsFixture(t *testing.T, format int) string {
	t.Helper()
	dir := t.TempDir()
	fileName := filepath.Join(dir, IndexFilename)

	creator, err := NewWriter(context.Background(), format, fileName)
	require.NoError(t, err)

	const numSeries = symbolFactor*4 + 5 // 133: several full sparse blocks plus a remainder.

	// Collect the sorted, de-duplicated symbol set (label name + all values).
	symbolSet := map[string]struct{}{"id": {}}
	values := make([]string, 0, numSeries)
	for i := range numSeries {
		v := fmt.Sprintf("v%04d", i)
		values = append(values, v)
		symbolSet[v] = struct{}{}
	}
	symbols := make([]string, 0, len(symbolSet))
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	for _, s := range symbols {
		require.NoError(t, creator.AddSymbol(s))
	}

	type entry struct {
		ls     labels.Labels
		chunks []ChunkMeta
	}
	entries := make([]entry, 0, numSeries)
	for _, v := range values {
		entries = append(entries, entry{
			ls:     labels.FromStrings("id", v),
			chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}},
		})
	}
	// AddSeries requires ascending fingerprint order.
	sort.Slice(entries, func(i, j int) bool {
		return labels.StableHash(entries[i].ls) < labels.StableHash(entries[j].ls)
	})
	for i, e := range entries {
		require.NoError(t, creator.AddSeries(
			storage.SeriesRef(i+1),
			e.ls,
			model.Fingerprint(labels.StableHash(e.ls)),
			e.chunks...,
		))
	}

	_, err = creator.Close(false)
	require.NoError(t, err)
	return fileName
}

// TestStreamSymbols_LookupMatchesMmap asserts that streamSymbols.Lookup and
// Symbols.Lookup have matching behavior.
func TestStreamSymbols_LookupMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			fn := writeManySymbolsFixture(t, format)

			mmap, err := NewMmapFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, mmap.Close()) })

			stream, err := NewStreamFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })

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
