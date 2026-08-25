package tsdb

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestOpenAndInspectIndexes(t *testing.T) {
	path := buildTSDBIndexFixture(t)

	results, err := OpenAndInspectIndexes([]string{path})
	require.NoError(t, err)
	require.Len(t, results, 1)

	result := results[0]
	t.Cleanup(func() {
		if result.Index != nil {
			_ = result.Index.Close()
		}
	})

	require.Equal(t, path, result.LocalPath)
	require.Equal(t, tsdbindex.FormatV3, result.Version)
	require.NotEmpty(t, result.LabelNames)
	require.Contains(t, result.LabelNames, "service_name")
	require.Greater(t, int64(result.Bounds[1]), int64(result.Bounds[0]))
	require.NotNil(t, result.Index, "Index handle should be non-nil for successful open")
}

func TestReaderCopiesStringsBeforeClose(t *testing.T) {
	path := buildTSDBIndexFixture(t)

	results, err := OpenAndInspectIndexes([]string{path})
	require.NoError(t, err)
	require.Len(t, results, 1)
	t.Cleanup(func() {
		if results[0].Index != nil {
			_ = results[0].Index.Close()
		}
	})
	require.NotEmpty(t, results[0].LabelNames)

	runtime.GC()
	runtime.GC()

	require.Contains(t, results[0].LabelNames, "service_name")
	require.Contains(t, results[0].LabelNames, "namespace")
}

func TestOpenAndInspectIndexesErrorIncludesPath(t *testing.T) {
	missing := t.TempDir() + "/missing.tsdb"

	_, err := OpenAndInspectIndexes([]string{missing})
	require.Error(t, err)
	require.Contains(t, err.Error(), missing)
}

func buildTSDBIndexFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	b := tsdb.NewBuilder(tsdbindex.FormatV3)

	ls := labels.FromStrings("service_name", "querier", "namespace", "loki")
	b.AddSeries(ls, model.Fingerprint(labels.StableHash(ls)), []tsdbindex.ChunkMeta{
		{
			Checksum: 1,
			MinTime:  int64(model.TimeFromUnixNano(time.Date(2026, 3, 2, 1, 0, 0, 0, time.UTC).UnixNano())),
			MaxTime:  int64(model.TimeFromUnixNano(time.Date(2026, 3, 2, 2, 0, 0, 0, time.UTC).UnixNano())),
			KB:       1,
			Entries:  10,
		},
	})

	id, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
		return tsdb.NewPrefixedIdentifier(tsdb.SingleTenantTSDBIdentifier{
			TS:       time.Unix(0, 0),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}, dir, "")
	})
	require.NoError(t, err)

	return id.Path()
}
