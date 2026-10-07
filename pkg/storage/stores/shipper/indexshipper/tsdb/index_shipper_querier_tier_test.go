package tsdb

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestTierNamesMatchAccessTiers(t *testing.T) {
	// AccessStats compares tiers by name, so these must stay in sync.
	require.Equal(t, shipperindex.AccessTierMemory, string(index.TierMemory))
	require.Equal(t, shipperindex.AccessTierDisk, string(index.TierDisk))
}

func TestTSDBFile_Tier(t *testing.T) {
	built := BuildIndex(t, t.TempDir(), []LoadableSeries{
		{Labels: mustParseLabels(`{foo="bar"}`), Chunks: buildChunkMetas(0, 10)},
	})
	defer built.Close()

	for name, tc := range map[string]struct {
		opts index.ReaderOptions
		want index.Tier
	}{
		"mmap":   {opts: index.MmapOptions{}, want: index.TierDisk},
		"stream": {opts: index.DefaultStreamOptions(), want: index.TierDisk},
		"in memory": {
			opts: index.InMemoryOptions{Budget: index.NewMemoryBudget(1<<30, nil), Fallback: index.MmapOptions{}},
			want: index.TierMemory,
		},
		"in memory over budget": {
			opts: index.InMemoryOptions{Budget: index.NewMemoryBudget(1, nil), Fallback: index.DefaultStreamOptions()},
			want: index.TierDisk,
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := NewShippableTSDBFile(built.Identifier, tc.opts)
			require.NoError(t, err)
			defer f.Close()
			require.Equal(t, tc.want, f.Tier())
		})
	}
}

func TestIndexShipperQuerier_RecordsFileAccesses(t *testing.T) {
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}
	indexStart := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	series := []LoadableSeries{
		{Labels: mustParseLabels(`{foo="bar"}`), Chunks: buildChunkMetas(int64(indexStart), int64(indexStart+99))},
	}

	onDisk := BuildIndex(t, t.TempDir(), series)
	defer onDisk.Close()
	built := BuildIndex(t, t.TempDir(), series)
	defer built.Close()
	inMemory, err := NewShippableTSDBFile(built.Identifier, index.InMemoryOptions{
		Budget:   index.NewMemoryBudget(1<<30, nil),
		Fallback: index.MmapOptions{},
	})
	require.NoError(t, err)
	defer inMemory.Close()
	require.Equal(t, index.TierMemory, inMemory.Tier())

	matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
	table := tableRange.PeriodConfig.IndexTables.TableFor(indexStart)

	for name, tc := range map[string]struct {
		files                []*TSDBFile
		wantMemory, wantDisk int64
		wantTier             string
	}{
		"memory only":     {files: []*TSDBFile{inMemory}, wantMemory: 1, wantTier: shipperindex.AccessTierMemory},
		"disk only":       {files: []*TSDBFile{onDisk}, wantDisk: 1, wantTier: shipperindex.AccessTierDisk},
		"memory and disk": {files: []*TSDBFile{inMemory, onDisk}, wantMemory: 1, wantDisk: 1, wantTier: shipperindex.AccessTierDisk},
		"no files":        {wantTier: shipperindex.AccessTierNone},
	} {
		t.Run(name, func(t *testing.T) {
			q := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: map[string][]*TSDBFile{table: tc.files}}, tableRange)

			ctx, stats := shipperindex.NewContextWithAccessStats(context.Background())
			_, err := q.GetChunkRefs(ctx, "fake", indexStart, indexStart+100, nil, nil, matcher)
			require.NoError(t, err)

			memory, disk := stats.FileAccesses()
			require.Equal(t, tc.wantMemory, memory)
			require.Equal(t, tc.wantDisk, disk)
			require.Equal(t, tc.wantTier, stats.RequestTier())

			// Without AccessStats in the context nothing is recorded, and the query still works.
			_, err = q.GetChunkRefs(context.Background(), "fake", indexStart, indexStart+100, nil, nil, matcher)
			require.NoError(t, err)
		})
	}
}
