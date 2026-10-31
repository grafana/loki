package hintprovider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

const (
	v5TestInterval  = 16 * time.Second
	v5TestShardBits = 5
)

// v5Cell returns the cell of bucket and document shard in the v5 test layout.
func v5Cell(bucket, documentShard uint32) uint32 {
	return bucket<<v5TestShardBits | documentShard
}

// writeV5TestIndex stores a v5 index whose footer covers layoutDay and whose
// meta.json covers metaDay, with each of needle's terms posted to cells.
func writeV5TestIndex(t *testing.T, indexStore *store.Store, hash, needle string, layoutDay, metaDay time.Time, cells []uint32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v5.lidx")
	writer, err := logline.NewWriter("v5", path, nil, &format.WriterConfig{
		DensityThreshold:  -1,
		DocumentInterval:  v5TestInterval,
		DocumentShardBits: v5TestShardBits,
		DayStart:          layoutDay,
	})
	require.NoError(t, err)

	terms, err := ExtractQueryNgrams(needle, 6, "v5")
	require.NoError(t, err)
	require.NotEmpty(t, terms)
	keys := make([][8]byte, 0, len(terms))
	for _, term := range terms {
		var key [8]byte
		copy(key[:], term)
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b [8]byte) int { return bytes.Compare(a[:], b[:]) })
	for _, key := range slices.Compact(keys) {
		require.NoError(t, writer.WriteTermDocIDs(key, cells, len(cells)))
	}
	require.NoError(t, writer.Close())
	info := writer.Info()

	indexBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	metaLayout := format.DocumentLayout{Interval: v5TestInterval, ShardBits: v5TestShardBits, DayStart: metaDay}
	minLogTS, _ := metaLayout.TimeRange(cells[0])
	_, maxLogTS := metaLayout.TimeRange(cells[len(cells)-1])
	maxLogTS = maxLogTS.Add(-time.Millisecond)
	meta := store.Meta{
		Date:              metaDay.Format(time.DateOnly),
		Hash:              hash,
		Version:           "v5",
		MinLogTs:          minLogTS,
		MaxLogTs:          maxLogTS,
		MinRecordTs:       minLogTS,
		MaxRecordTs:       maxLogTS,
		IndexHeader:       &info,
		SizeBytes:         int64(len(indexBytes)),
		DocumentInterval:  v5TestInterval,
		DocumentShardBits: v5TestShardBits,
	}
	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}

// TestLoglineHintProvider_ProvideHints_V5Cells checks that v5 hints come from
// the footer's layout and the matching cells: cells of one bucket in
// different document shards give one time-only range.
func TestLoglineHintProvider_ProvideHints_V5Cells(t *testing.T) {
	const needle = "9fA81cD2Ef0077aa"
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	// 10:00:00 is bucket 2250 of the day and 10:02:40 is bucket 2260.
	indexStore := newTestStore(t)
	writeV5TestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, day, day, []uint32{v5Cell(2250, 3), v5Cell(2250, 17), v5Cell(2260, 3)})

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	t0 := day.Add(10 * time.Hour)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`),
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(5*time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 2)
	require.Equal(t, t0, hints.TimeRanges[0].Start)
	require.Equal(t, t0.Add(16*time.Second), hints.TimeRanges[0].End)
	require.Equal(t, t0.Add(160*time.Second), hints.TimeRanges[1].Start)
	require.Equal(t, t0.Add(176*time.Second), hints.TimeRanges[1].End)
}

// TestLoglineHintProvider_ProvideHints_V5DayMismatchFails checks that a v5
// footer whose day is not the meta's date fails the lookup, so the query runs
// without hints, rather than producing ranges on the wrong day.
func TestLoglineHintProvider_ProvideHints_V5DayMismatchFails(t *testing.T) {
	const needle = "9fA81cD2Ef0077aa"
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	indexStore := newTestStore(t)
	writeV5TestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, day.AddDate(0, 0, 1), day, []uint32{v5Cell(2250, 3)})

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	t0 := day.Add(10 * time.Hour)
	_, _, err = provider.ProvideHints(
		context.Background(),
		"test-tenant",
		mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`),
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(time.Minute).UnixNano()),
	)
	require.ErrorContains(t, err, "footer day 2026-02-27 differs from meta date 2026-02-26")
}

func TestRangesForCells(t *testing.T) {
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	layout := format.DocumentLayout{Interval: v5TestInterval, ShardBits: v5TestShardBits, DayStart: day}
	meta := store.Meta{Date: "2026-02-26", Hash: "aaaaaaaaaaaaaaaa"}

	ranges := rangesForCells(meta, layout, []uint32{v5Cell(0, 0), v5Cell(0, 1), v5Cell(0, 31), v5Cell(1, 0), v5Cell(5399, 31)})
	require.Len(t, ranges, 3)
	for i, want := range []struct{ start, end time.Time }{
		{day, day.Add(16 * time.Second)},
		{day.Add(16 * time.Second), day.Add(32 * time.Second)},
		{day.Add(24*time.Hour - 16*time.Second), day.Add(24 * time.Hour)},
	} {
		require.Equal(t, want.start, ranges[i].Start, "range %d", i)
		require.Equal(t, want.end, ranges[i].End, "range %d", i)
	}
	require.Contains(t, ranges[0].Source, "doc=0,")
	require.Contains(t, ranges[2].Source, "doc=172799,")
}

func TestCheckDocumentLayout(t *testing.T) {
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	layout := format.DocumentLayout{Interval: v5TestInterval, ShardBits: v5TestShardBits, DayStart: day}

	for _, tt := range []struct {
		name string
		meta store.Meta
		want string
	}{
		{name: "matching meta", meta: store.Meta{Date: "2026-02-26", DocumentInterval: v5TestInterval, DocumentShardBits: v5TestShardBits}},
		{name: "wire meta without a layout", meta: store.Meta{Date: "2026-02-26"}},
		{name: "different date", meta: store.Meta{Date: "2026-02-25"}, want: "footer day 2026-02-26 differs from meta date 2026-02-25"},
		{name: "different interval", meta: store.Meta{Date: "2026-02-26", DocumentInterval: 8 * time.Second, DocumentShardBits: v5TestShardBits}, want: "footer document interval 16s differs"},
		{name: "different shard bits", meta: store.Meta{Date: "2026-02-26", DocumentInterval: v5TestInterval, DocumentShardBits: 4}, want: "footer document shard bits 5 differ"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDocumentLayout(tt.meta, layout)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// TestLoglineHintProvider_ProvideHints_MixedV3AndV5 covers the realistic
// rollout state: one day holding both v3 and v5 indexes. executeQuery picks
// rangesForDocIDs or rangesForCells per reader, so a layout flag wired to the
// wrong reader in the concurrent openReadersForMetas loop would hand v3
// document IDs to TimeRange and produce ranges on a nonsense bucket. Both
// indexes must contribute their own correct range.
func TestLoglineHintProvider_ProvideHints_MixedV3AndV5(t *testing.T) {
	const needle = "9fA81cD2Ef0077aa"
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	indexStore := newTestStore(t)

	// v5 index: bucket 2250 is 10:00:00, two document shards of one bucket.
	writeV5TestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, day, day, []uint32{v5Cell(2250, 3), v5Cell(2250, 17)})
	// v3 index on the same day, a different hour, mapping through its table.
	v3Min := day.Add(14 * time.Hour)
	v3Max := v3Min.Add(30 * time.Second)
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, v3Min, v3Max)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`),
		model.TimeFromUnixNano(day.UnixNano()),
		model.TimeFromUnixNano(day.Add(24*time.Hour).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)

	starts := make([]time.Time, 0, len(hints.TimeRanges))
	for _, r := range hints.TimeRanges {
		starts = append(starts, r.Start.UTC())
	}
	v5Start := day.Add(10 * time.Hour)
	require.Contains(t, starts, v5Start, "the v5 index must contribute its bucket's range")
	require.Contains(t, starts, v3Min.UTC(), "the v3 index must contribute its document's range")

	for _, r := range hints.TimeRanges {
		switch r.Start.UTC() {
		case v5Start:
			require.Equal(t, v5Start.Add(v5TestInterval), r.End.UTC(), "v5 ranges span one bucket")
		case v3Min.UTC():
			require.Equal(t, v3Max.UTC(), r.End.UTC(), "v3 ranges span the document's bounds")
		}
	}
}
