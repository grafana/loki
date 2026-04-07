package index

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

func newTestIndexBuilder(t *testing.T) *indexobj.Builder {
	t.Helper()
	builder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
	require.NoError(t, err)
	return builder
}

// makeTestStreamLabels creates a stream labels map with 3 streams.
// stream 1: service_name=svcA
// stream 2: service_name=svcB
// stream 3: (no service_name)
func makeTestStreamLabels() map[int64]labels.Labels {
	return map[int64]labels.Labels{
		1: labels.FromStrings("service_name", "svcA", "env", "prod"),
		2: labels.FromStrings("service_name", "svcB", "env", "dev"),
		3: labels.FromStrings("env", "staging"),
	}
}

func makeTestCalcContext(builder *indexobj.Builder) *logsCalculationContext {
	return &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}
}

func flushStatsForTenant(t *testing.T, builder *indexobj.Builder, tenantID string) []stats.Stat {
	t.Helper()
	sb := builder.StatsBuilderForTenant(tenantID)
	if sb == nil {
		return nil
	}
	sections, err := sb.Flush(context.Background())
	require.NoError(t, err)

	var allStats []stats.Stat
	for _, sec := range sections {
		rr, err := stats.NewRowReader(&sec)
		require.NoError(t, err)
		defer rr.Close()

		buf := make([]stats.Stat, 64)
		for {
			n, err := rr.Read(context.Background(), buf)
			allStats = append(allStats, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
	}
	return allStats
}

func TestStatsCalculation_BasicAggregation(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	line1 := []byte("hello world")
	line2 := []byte("another line")
	line3 := []byte("from svcB")
	line4 := []byte("x")

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: line1},                    // svcA, 11 bytes
		{StreamID: 1, Timestamp: ts2, Line: line2},                    // svcA, 12 bytes
		{StreamID: 2, Timestamp: ts3, Line: line3},                    // svcB, 9 bytes
		{StreamID: 3, Timestamp: time.Unix(50, 0).UTC(), Line: line4}, // no service_name, 1 byte
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	got := flushStatsForTenant(t, builder, "tenant-1")
	// We expect 3 aggregates: "", "svcA", "svcB" (sorted)
	require.Len(t, got, 3)

	// Sorted order: "" < "svcA" < "svcB"
	require.Equal(t, "", got[0].Labels["service_name"])
	require.Equal(t, int64(1), got[0].RowCount)
	require.Equal(t, int64(len(line4)), got[0].UncompressedSize)

	require.Equal(t, "svcA", got[1].Labels["service_name"])
	require.Equal(t, int64(2), got[1].RowCount)
	require.Equal(t, int64(len(line1)+len(line2)), got[1].UncompressedSize)
	require.Equal(t, ts1.UnixNano(), got[1].MinTimestamp)
	require.Equal(t, ts2.UnixNano(), got[1].MaxTimestamp)

	require.Equal(t, "svcB", got[2].Labels["service_name"])
	require.Equal(t, int64(1), got[2].RowCount)
	require.Equal(t, int64(len(line3)), got[2].UncompressedSize)
	require.Equal(t, ts3.UnixNano(), got[2].MinTimestamp)
	require.Equal(t, ts3.UnixNano(), got[2].MaxTimestamp)
}

func TestStatsCalculation_MetadataFields(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts := time.Unix(500, 0).UTC()
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts, Line: []byte("msg")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	got := flushStatsForTenant(t, builder, "tenant-1")
	require.Len(t, got, 1)

	// Verify metadata fields are set correctly.
	s := got[0]
	require.Equal(t, "test/path/obj1", s.ObjectPath)
	require.Equal(t, int64(0), s.SectionIndex)
	require.Equal(t, "service_name", s.SortSchema)
	require.Equal(t, "svcA", s.Labels["service_name"])
}

func TestStatsCalculation_MissingServiceName(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	batch := []logs.Record{
		{StreamID: 3, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("no svc")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	got := flushStatsForTenant(t, builder, "tenant-1")
	require.Len(t, got, 1)
	require.Equal(t, "", got[0].Labels["service_name"])
}

func TestStatsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	// First batch.
	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("first")},
		{StreamID: 2, Timestamp: time.Unix(20, 0).UTC(), Line: []byte("second")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch1))

	// Second batch — extends svcA's time range and adds more bytes.
	batch2 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(30, 0).UTC(), Line: []byte("third batch")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch2))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	got := flushStatsForTenant(t, builder, "tenant-1")
	// svcA and svcB
	require.Len(t, got, 2)

	var svcA stats.Stat
	for _, s := range got {
		if s.Labels["service_name"] == "svcA" {
			svcA = s
		}
	}
	require.Equal(t, int64(2), svcA.RowCount)
	require.Equal(t, time.Unix(10, 0).UTC().UnixNano(), svcA.MinTimestamp)
	require.Equal(t, time.Unix(30, 0).UTC().UnixNano(), svcA.MaxTimestamp)
	require.Equal(t, int64(len("first")+len("third batch")), svcA.UncompressedSize)
}

func TestStatsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, nil))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	// No data → no stats.
	sb := builder.StatsBuilderForTenant("tenant-1")
	require.Nil(t, sb, "expected no stats builder to be created for empty batch")
}
