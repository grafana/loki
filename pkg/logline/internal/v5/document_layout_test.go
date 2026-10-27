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

var sharedTerm = [8]byte{'s', 'h', 'a', 'r', 'e', 'd'}

// writeLayoutIndex writes an index with one term whose postings are cells,
// through the public NewWriter, recording the given document layout.
func writeLayoutIndex(t *testing.T, name string, interval time.Duration, shardBits int, dayStart time.Time, cells ...uint32) string {
	t.Helper()
	if len(cells) == 0 {
		cells = []uint32{0, 1}
	}
	path := filepath.Join(t.TempDir(), name)
	w, err := NewWriter(path, nil, &format.WriterConfig{
		DensityThreshold:  -1,
		DocumentInterval:  interval,
		DocumentShardBits: shardBits,
		DayStart:          dayStart,
	})
	require.NoError(t, err)
	require.NoError(t, w.WriteTermDocIDs(sharedTerm, cells, len(cells)))
	require.NoError(t, w.Close())
	return path
}

func TestFooter_DocumentLayoutRoundTrip(t *testing.T) {
	h, err := ReadIndexHeader(writeLayoutIndex(t, "sharded.lidx", 16*time.Second, 5, testDayStart))
	require.NoError(t, err)
	require.Equal(t, documentLayout{interval: 16 * time.Second, shardBits: 5, dayStartUnixNano: testDayStart.UnixNano()}, h.documentLayout())
	require.Equal(t, uint8(5), h.ReservedMid[8])
	require.Equal(t, IndexVersion, h.Version)

	h, err = ReadIndexHeader(writeLayoutIndex(t, "time-only.lidx", 16*time.Second, 0, testDayStart))
	require.NoError(t, err)
	require.Equal(t, documentLayout{interval: 16 * time.Second, dayStartUnixNano: testDayStart.UnixNano()}, h.documentLayout())

	r, err := OpenIndexFile(writeLayoutIndex(t, "public.lidx", 16*time.Second, 5, testDayStart))
	require.NoError(t, err)
	defer r.Close()
	require.Nil(t, r.Documents())
	layout := r.DocumentLayout()
	require.Equal(t, format.DocumentLayout{Interval: 16 * time.Second, ShardBits: 5, DayStart: testDayStart}, layout)
	require.Equal(t, uint64(172_800), layout.CellsPerDay())
}

func TestNewWriter_RequiresValidDocumentLayout(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  format.WriterConfig
		want string
	}{
		{name: "no interval", cfg: format.WriterConfig{DayStart: testDayStart}, want: "must be positive and divide 24h"},
		{name: "interval does not divide a day", cfg: format.WriterConfig{DocumentInterval: 7 * time.Second, DayStart: testDayStart}, want: "must be positive and divide 24h"},
		{name: "no day start", cfg: format.WriterConfig{DocumentInterval: 16 * time.Second}, want: "needs a day start"},
		{name: "day start is not midnight", cfg: format.WriterConfig{DocumentInterval: 16 * time.Second, DayStart: testDayStart.Add(time.Hour)}, want: "is not a UTC midnight"},
		{name: "too many cells", cfg: format.WriterConfig{DocumentInterval: time.Millisecond, DocumentShardBits: 7, DayStart: testDayStart}, want: "more than uint32 document IDs hold"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWriter(filepath.Join(t.TempDir(), "bad.lidx"), nil, &tt.cfg)
			require.ErrorContains(t, err, tt.want)
		})
	}

	t.Run("documents are refused", func(t *testing.T) {
		cfg := format.WriterConfig{DocumentInterval: 16 * time.Second, DayStart: testDayStart}
		_, err := NewWriter(filepath.Join(t.TempDir(), "docs.lidx"), []format.DocumentMetadata{{ID: 0}}, &cfg)
		require.ErrorContains(t, err, "no document table")
	})

	t.Run("cells outside the day are refused", func(t *testing.T) {
		cfg := format.WriterConfig{DensityThreshold: -1, DocumentInterval: 12 * time.Hour, DocumentShardBits: 1, DayStart: testDayStart}
		w, err := NewWriter(filepath.Join(t.TempDir(), "cells.lidx"), nil, &cfg)
		require.NoError(t, err)
		require.ErrorContains(t, w.WriteTermDocIDs(sharedTerm, []uint32{3, 4}, 2), "not a cell of the day")
		require.Error(t, w.Close())
	})
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
// shard-scaled cutoff: 10,000 cells is dense for one shard at 5s × 0.5
// (cutoff 8640) but not for four (cutoff 34,560).
func TestDensityFilter_DocumentShardBits(t *testing.T) {
	const n = 10_000
	ids := make([]uint32, n)
	for i := range ids {
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
		w, err := NewWriter(path, nil, &format.WriterConfig{
			DensityThreshold:  0.5,
			DocumentInterval:  5 * time.Second,
			DocumentShardBits: tt.shardBits,
			DayStart:          testDayStart,
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
	sharded := writeLayoutIndex(t, "a.lidx", 16*time.Second, 5, testDayStart)

	mergedPostings := func(t *testing.T, out *bytes.Buffer) (documentLayout, []uint32) {
		t.Helper()
		r, err := OpenIndexAt(bytes.NewReader(out.Bytes()), 0, int64(out.Len()))
		require.NoError(t, err)
		defer r.Close()
		ids, matchesAll, err := r.query(string(sharedTerm[:6]))
		require.NoError(t, err)
		require.False(t, matchesAll)
		return r.header.documentLayout(), ids
	}

	t.Run("sharded inputs merge with identity document IDs", func(t *testing.T) {
		var out bytes.Buffer
		inputs := []string{
			writeLayoutIndex(t, "a.lidx", 16*time.Second, 5, testDayStart, 0, 33, 64),
			writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, testDayStart, 1, 33, 172_799),
		}
		info, err := mergeFilesTo(ctx, t, inputs, &out, DefaultFastIndexWriteConfig())
		require.NoError(t, err)
		require.Equal(t, uint32(5), info.DocumentCount, "DocumentCount counts the distinct cells")
		layout, ids := mergedPostings(t, &out)
		require.Equal(t, documentLayout{interval: 16 * time.Second, shardBits: 5, dayStartUnixNano: testDayStart.UnixNano()}, layout)
		require.Equal(t, []uint32{0, 1, 33, 64, 172_799}, ids)
	})

	t.Run("a configured layout equal to the inputs' is accepted", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DocumentInterval = 16 * time.Second
		cfg.DocumentShardBits = 5
		cfg.DayStart = testDayStart
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, testDayStart)}, &out, cfg)
		require.NoError(t, err)
	})

	t.Run("different shard bits are rejected", func(t *testing.T) {
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 4, testDayStart)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "input 1 has document interval 16s, 4 document shard bits")
	})

	t.Run("different intervals are rejected", func(t *testing.T) {
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 8*time.Second, 5, testDayStart)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "input 1 has document interval 8s")
	})

	t.Run("different days are rejected", func(t *testing.T) {
		var out bytes.Buffer
		nextDay := testDayStart.AddDate(0, 0, 1)
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, nextDay)}, &out, DefaultFastIndexWriteConfig())
		require.ErrorContains(t, err, "day start "+nextDay.Format(time.RFC3339))
	})

	t.Run("configured shard bits that disagree with the inputs are rejected", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DocumentShardBits = 1
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, testDayStart)}, &out, cfg)
		require.ErrorContains(t, err, "configured document shard bits 1 differ from the inputs' 5")
	})

	t.Run("a configured day start that disagrees with the inputs is rejected", func(t *testing.T) {
		cfg := DefaultFastIndexWriteConfig()
		cfg.DayStart = testDayStart.AddDate(0, 0, -1)
		var out bytes.Buffer
		_, err := mergeFilesTo(ctx, t, []string{sharded, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, testDayStart)}, &out, cfg)
		require.ErrorContains(t, err, "configured day start")
	})
}

// v5 files written before day-relative cells have footer version 4 and
// dense-rank postings. Readers and the merge refuse them rather than decode
// their postings as cells.
func TestFooterVersion4IsRefused(t *testing.T) {
	path, info := rewriteFooter(t, writeLayoutIndex(t, "v4.lidx", 16*time.Second, 5, testDayStart), func(h *IndexFooter) { h.Version = 4 })

	_, err := OpenIndexFile(path)
	require.ErrorContains(t, err, "unsupported index version: 4")

	f, size := openTestReaderAt(t, path)
	_, err = OpenIndexAtWithHeader(f, 0, size, info)
	require.ErrorContains(t, err, "unsupported index version: 4")

	var out bytes.Buffer
	_, err = mergeFilesTo(context.Background(), t, []string{path, writeLayoutIndex(t, "b.lidx", 16*time.Second, 5, testDayStart)}, &out, DefaultFastIndexWriteConfig())
	require.ErrorContains(t, err, "unsupported index version: 4")
}
