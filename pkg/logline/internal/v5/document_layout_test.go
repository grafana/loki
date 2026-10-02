package v5

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// writeLayoutIndex writes a two-document index with one term through the
// public NewWriter, recording the given document layout.
func writeLayoutIndex(t *testing.T, name string, interval time.Duration, shardBits int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 16_000},
		{ID: 1, MinTimeUnix: 0, MaxTimeUnix: 16_000},
	}
	w, err := NewWriter(path, docs, &format.WriterConfig{
		DensityThreshold:  -1,
		DocumentInterval:  interval,
		DocumentShardBits: shardBits,
	})
	require.NoError(t, err)
	require.NoError(t, w.WriteTermDocIDs([8]byte{'s', 'h', 'a', 'r', 'e', 'd'}, []uint32{0, 1}, 2))
	require.NoError(t, w.Close())
	return path
}

func TestFooter_DocumentLayoutRoundTrip(t *testing.T) {
	h, err := ReadIndexHeader(writeLayoutIndex(t, "sharded.lidx", 16*time.Second, 5))
	require.NoError(t, err)
	require.Equal(t, documentLayout{interval: 16 * time.Second, shardBits: 5}, h.documentLayout())
	require.Equal(t, uint8(5), h.ReservedMid[8])
	require.Equal(t, IndexVersion, h.Version, "the layout rides in ReservedMid; the footer version is unchanged")

	h, err = ReadIndexHeader(writeLayoutIndex(t, "time-only.lidx", 16*time.Second, 0))
	require.NoError(t, err)
	require.Equal(t, documentLayout{interval: 16 * time.Second}, h.documentLayout())

	h, err = ReadIndexHeader(writeLayoutIndex(t, "unset.lidx", 0, 0))
	require.NoError(t, err)
	require.Equal(t, documentLayout{}, h.documentLayout())
}

func TestSentinelCutoff_ScalesWithDocumentShardBits(t *testing.T) {
	for _, tt := range []struct {
		name      string
		shardBits int
		want      uint64
	}{
		{name: "one shard", shardBits: 0, want: 8640},
		{name: "four shards", shardBits: 2, want: 4 * 8640},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := IndexWriteConfig{DensityThreshold: 0.5, DocumentInterval: 5 * time.Second, DocumentShardBits: tt.shardBits}
			cutoff, ok := cfg.sentinelCutoff()
			require.True(t, ok)
			require.Equal(t, tt.want, cutoff)
		})
	}

	_, ok := IndexWriteConfig{DensityThreshold: 0.5}.sentinelCutoff()
	require.False(t, ok, "zero interval disables the density filter")
}

// TestDensityFilter_DocumentShardBits checks the writer applies the
// shard-scaled cutoff: 10,000 documents is dense for one shard at 5s × 0.5
// (cutoff 8640) but not for four (cutoff 34,560).
func TestDensityFilter_DocumentShardBits(t *testing.T) {
	const n = 10_000
	docs := make([]format.DocumentMetadata, n)
	ids := make([]uint32, n)
	for i := range docs {
		docs[i] = format.DocumentMetadata{ID: uint32(i), MinTimeUnix: int64(i), MaxTimeUnix: int64(i + 1)}
		ids[i] = uint32(i)
	}
	term := [8]byte{'d', 'e', 'n', 's', 'e', '!'}

	for _, tt := range []struct {
		shardBits      int
		wantMatchesAll bool
	}{
		{shardBits: 0, wantMatchesAll: true},
		{shardBits: 2, wantMatchesAll: false},
	} {
		path := filepath.Join(t.TempDir(), "density.lidx")
		w, err := NewWriter(path, docs, &format.WriterConfig{
			DensityThreshold:  0.5,
			DocumentInterval:  5 * time.Second,
			DocumentShardBits: tt.shardBits,
		})
		require.NoError(t, err)
		require.NoError(t, w.WriteTermDocIDs(term, ids, n))
		require.NoError(t, w.Close())

		r, err := OpenIndexFile(path)
		require.NoError(t, err)
		it, err := r.NewTermIterator()
		require.NoError(t, err)
		require.True(t, it.Next())
		require.Equal(t, tt.wantMatchesAll, it.Bitmap().MatchesAll, "shardBits=%d", tt.shardBits)
		require.NoError(t, r.Close())
	}
}

func TestMerge_DocumentLayoutGuard(t *testing.T) {
	ctx := context.Background()
	sharded := writeLayoutIndex(t, "a.lidx", 16*time.Second, 5)

	t.Run("equal time-only layouts merge and keep the layout", func(t *testing.T) {
		var out bytes.Buffer
		inputs := []string{writeLayoutIndex(t, "a.lidx", 16*time.Second, 0), writeLayoutIndex(t, "b.lidx", 16*time.Second, 0)}
		_, err := mergeFilesTo(ctx, t, inputs, &out, DefaultFastIndexWriteConfig())
		require.NoError(t, err)
		h, err := ReadIndexFooterFrom(bytes.NewReader(out.Bytes()), int64(out.Len()))
		require.NoError(t, err)
		require.Equal(t, documentLayout{interval: 16 * time.Second}, h.documentLayout())
	})

	t.Run("sharded inputs are rejected", func(t *testing.T) {
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "indexes with 5 document shard bits cannot be merged")
	})

	t.Run("a sharded config over inputs without a layout is rejected", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DocumentInterval = 16 * time.Second
		cfg.DocumentShardBits = 5
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{writeLayoutIndex(t, "a.lidx", 0, 0), writeLayoutIndex(t, "b.lidx", 0, 0)}, &out, cfg)
		require.ErrorContains(t, err, "indexes with 5 document shard bits cannot be merged")
	})

	t.Run("different shard bits are rejected", func(t *testing.T) {
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 4)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "input 1 has document interval 16s and 4 document shard bits")
	})

	t.Run("different intervals are rejected", func(t *testing.T) {
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 8*time.Second, 5)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "input 1 has document interval 8s")
	})

	t.Run("config that disagrees with the inputs is rejected", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DocumentShardBits = 1
		var out bytes.Buffer
		inputs := []string{writeLayoutIndex(t, "a.lidx", 16*time.Second, 0), writeLayoutIndex(t, "b.lidx", 16*time.Second, 0)}
		_, err := mergeFilesTo(ctx, t, inputs, &out, cfg)
		require.ErrorContains(t, err, "configured document shard bits 1 differ from the inputs' 0")
	})

	t.Run("inputs without a layout keep the config", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DocumentInterval = 16 * time.Second
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{writeLayoutIndex(t, "a.lidx", 0, 0), writeLayoutIndex(t, "b.lidx", 0, 0)}, &out, cfg)
		require.NoError(t, err)
		h, err := ReadIndexFooterFrom(bytes.NewReader(out.Bytes()), int64(out.Len()))
		require.NoError(t, err)
		require.Equal(t, documentLayout{interval: 16 * time.Second}, h.documentLayout())
	})
}
