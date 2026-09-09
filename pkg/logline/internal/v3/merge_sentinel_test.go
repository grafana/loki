package v3

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestMergeSentinelPropagation(t *testing.T) {
	// Sentinel from any source must propagate to merged output.
	// Once a term is density-filtered, it stays filtered forever.
	dir := t.TempDir()

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0 // don't auto-filter during merge write

	// Index A: term "aaaaaa" is sentinel (explicitly written)
	pathA := filepath.Join(dir, "a.lidx")
	wA, err := newStreamingIndexWriter(pathA, cfg, 2)
	require.NoError(t, err)
	wA.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
	})
	require.NoError(t, wA.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{MatchesAll: true})) // sentinel
	bmA := roaring.New()
	bmA.Add(0)
	require.NoError(t, wA.WriteTermBitmap([8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, format.Bitmap{Roaring: bmA}))
	require.NoError(t, wA.Close())

	// Index B: term "aaaaaa" is real (3 docs in B)
	pathB := filepath.Join(dir, "b.lidx")
	wB, err := newStreamingIndexWriter(pathB, cfg, 3)
	require.NoError(t, err)
	wB.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 2, MaxTimeUnix: 3},
		{ID: 1, MinTimeUnix: 3, MaxTimeUnix: 4},
		{ID: 2, MinTimeUnix: 4, MaxTimeUnix: 5},
	})
	bmBa := roaring.New()
	bmBa.AddMany([]uint32{0, 1, 2})
	require.NoError(t, wB.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{Roaring: bmBa})) // real
	bmBb := roaring.New()
	bmBb.Add(1)
	require.NoError(t, wB.WriteTermBitmap([8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, format.Bitmap{Roaring: bmBb}))
	require.NoError(t, wB.Close())

	// Merge A + B
	outputPath := filepath.Join(dir, "merged.lidx")
	out, err := os.Create(outputPath)
	require.NoError(t, err)
	_, err = mergeFilesTo(context.Background(), t, []string{pathA, pathB}, out, cfg)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	r, err := OpenIndexFile(outputPath)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)

	// Term "aaaaaa": sentinel from A wins, even though B had real data
	require.True(t, it.Next())
	require.Equal(t, [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, it.Term())
	require.True(t, it.Bitmap().MatchesAll, "sentinel from source A must propagate to merged output")

	// Term "bbbbbb": both real → merged bitmap
	require.True(t, it.Next())
	require.Equal(t, [8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, it.Term())
	require.False(t, it.Bitmap().MatchesAll, "non-sentinel term must remain non-sentinel in merge")

	require.False(t, it.Next())
}

func TestMergeBothSentinel(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0

	pathA := filepath.Join(dir, "a.lidx")
	wA, err := newStreamingIndexWriter(pathA, cfg, 2)
	require.NoError(t, err)
	wA.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
	})
	require.NoError(t, wA.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{MatchesAll: true}))
	require.NoError(t, wA.Close())

	pathB := filepath.Join(dir, "b.lidx")
	wB, err := newStreamingIndexWriter(pathB, cfg, 2)
	require.NoError(t, err)
	wB.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 2, MaxTimeUnix: 3},
		{ID: 1, MinTimeUnix: 3, MaxTimeUnix: 4},
	})
	require.NoError(t, wB.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a', 0, 0}, format.Bitmap{MatchesAll: true}))
	require.NoError(t, wB.Close())

	outputPath := filepath.Join(dir, "merged.lidx")
	out, err := os.Create(outputPath)
	require.NoError(t, err)
	_, err = mergeFilesTo(context.Background(), t, []string{pathA, pathB}, out, cfg)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	r, err := OpenIndexFile(outputPath)
	require.NoError(t, err)
	defer r.Close()
	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.True(t, it.Bitmap().MatchesAll, "both-sentinel merge must produce sentinel")
	require.False(t, it.Next())
}
