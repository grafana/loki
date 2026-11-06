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
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type shipperFile struct {
	multitenant bool
	idx         *TSDBFile
}

// mockTableIterator is like mockIndexShipperIndexIterator, but reports which
// files are multi-tenant.
type mockTableIterator struct {
	tables map[string][]shipperFile
}

func (m mockTableIterator) ForEachConcurrent(_ context.Context, tableName, _ string, callback shipperindex.ForEachIndexCallback) error {
	for _, f := range m.tables[tableName] {
		if err := callback(f.multitenant, f.idx); err != nil {
			return err
		}
	}
	return nil
}

// buildNamedIndex builds an index named like the compactor (per-tenant) or an
// ingester (multi-tenant) would name it, created at ts.
func buildNamedIndex(t *testing.T, multitenant bool, ts time.Time, chks ...index.ChunkMeta) shipperFile {
	ls := mustParseLabels(`{foo="bar"}`)
	fp := model.Fingerprint(labels.StableHash(ls))
	if multitenant {
		ls = labels.NewBuilder(ls).Set(TenantLabel, "fake").Labels()
	}

	b := NewBuilder(index.FormatV3)
	b.AddSeries(ls, fp, chks)

	dir := t.TempDir()
	id, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		if multitenant {
			return NewPrefixedIdentifier(MultitenantTSDBIdentifier{NodeName: "ingester-0", Ts: ts}, dir, "")
		}
		return NewPrefixedIdentifier(SingleTenantTSDBIdentifier{TS: ts, From: from, Through: through, Checksum: checksum}, dir, "")
	})
	require.NoError(t, err)

	idx, err := NewShippableTSDBFile(id, index.MmapOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return shipperFile{multitenant: multitenant, idx: idx}
}

func TestIndexShipperQuerier_SkipSuperseded(t *testing.T) {
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
	tableStart := time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod)
	from := model.TimeFromUnixNano(tableStart.UnixNano())
	through := from.Add(time.Hour)
	tableName := tableRange.PeriodConfig.IndexTables.TableFor(from)

	chk := func(i int) index.ChunkMeta {
		start := int64(from) + int64(i)*10
		return index.ChunkMeta{MinTime: start, MaxTime: start + 9, Checksum: uint32(i), KB: 1, Entries: 10}
	}

	// The newest compacted file holds chunks 1-3, compacted from the older
	// compacted file and the multi-tenant files listed by the compactor.
	compactedAt := tableStart.Add(3 * time.Hour)
	superseded := []shipperFile{
		buildNamedIndex(t, false, compactedAt.Add(-time.Hour), chk(1)),
		buildNamedIndex(t, true, compactedAt.Add(-2*time.Hour), chk(2)),
	}
	kept := []shipperFile{
		buildNamedIndex(t, false, compactedAt, chk(1), chk(2), chk(3)),
		// Compacted, but within the margin, so it is counted again.
		buildNamedIndex(t, true, compactedAt.Add(-30*time.Minute), chk(3)),
		// Not compacted yet.
		buildNamedIndex(t, true, compactedAt.Add(time.Minute), chk(4)),
	}
	// Uploaded too late for the compactor to read it, but named as if it had
	// been: Stats and Volume miss it until the next compaction.
	late := buildNamedIndex(t, true, compactedAt.Add(-2*time.Hour), chk(5))

	files := append(append(append([]shipperFile{}, superseded...), kept...), late)
	iter := mockTableIterator{tables: map[string][]shipperFile{tableName: files}}
	matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")

	statsFor := func(t *testing.T, q Index) stats.Stats {
		acc := stats.BloomPool.Get()
		defer stats.BloomPool.Put(acc)
		require.NoError(t, q.Stats(context.Background(), "fake", from, through, acc, nil, nil, matcher))
		return acc.Stats()
	}
	volumeFor := func(t *testing.T, q Index) uint64 {
		acc := seriesvolume.NewAccumulator(10, 10)
		require.NoError(t, q.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matcher))
		vols := acc.Volumes().Volumes
		require.Len(t, vols, 1)
		return vols[0].Volume
	}

	t.Run("Stats and Volume skip superseded files", func(t *testing.T) {
		q := newIndexShipperQuerier(iter, tableRange, true)
		// chunks 1, 2, 3 from the newest compacted file, 3 again and 4.
		require.Equal(t, stats.Stats{Streams: 1, Chunks: 5, Bytes: 5 << 10, Entries: 50}, statsFor(t, q))
		require.Equal(t, uint64(5<<10), volumeFor(t, q))
	})

	t.Run("disabled", func(t *testing.T) {
		q := newIndexShipperQuerier(iter, tableRange, false)
		require.Equal(t, stats.Stats{Streams: 1, Chunks: 8, Bytes: 8 << 10, Entries: 80}, statsFor(t, q))
		require.Equal(t, uint64(8<<10), volumeFor(t, q))
	})

	t.Run("without a compacted file nothing is skipped", func(t *testing.T) {
		q := newIndexShipperQuerier(mockTableIterator{tables: map[string][]shipperFile{tableName: {superseded[1], kept[1], kept[2], late}}}, tableRange, true)
		require.Equal(t, stats.Stats{Streams: 1, Chunks: 4, Bytes: 4 << 10, Entries: 40}, statsFor(t, q))
	})

	t.Run("GetChunkRefs reads every file", func(t *testing.T) {
		q := newIndexShipperQuerier(iter, tableRange, true)
		refs, err := q.GetChunkRefs(context.Background(), "fake", from, through, nil, nil, matcher)
		require.NoError(t, err)
		require.Len(t, refs, 5)
	})
}
