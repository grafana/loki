package index

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
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

func flushAndReadAllStatsTable(t *testing.T, builder *indexobj.Builder) arrowtest.Rows {
	t.Helper()
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var all arrowtest.Rows
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err := stats.Open(context.Background(), s)
		require.NoError(t, err)

		r := stats.NewReader(stats.ReaderOptions{
			Columns:   sec.Columns(),
			Allocator: memory.DefaultAllocator,
		})
		require.NoError(t, r.Open(context.Background()))
		t.Cleanup(func() { _ = r.Close() })

		tbl, err := readStatsTable(context.Background(), r)
		if errors.Is(err, io.EOF) {
			continue
		}
		require.NoError(t, err)

		rows, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
		require.NoError(t, err)
		all = append(all, rows...)
	}
	return all
}

func readStatsTable(ctx context.Context, r *stats.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil && rec.NumRows() > 0 {
			recs = append(recs, rec)
		}
		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}
	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}

func TestStatsCalculation_BasicAggregation(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

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

	actual := flushAndReadAllStatsTable(t, builder)
	// We expect 3 aggregates: "", "svcA", "svcB" (sorted)
	require.Len(t, actual, 3)

	// Sorted order: "" < "svcA" < "svcB"
	require.Equal(t, arrowtest.Row{
		"object_path.utf8":        "test/path/obj1",
		"section_index.int64":     int64(0),
		"sort_schema.utf8":        "service_name",
		"service_name.label.utf8": "",
		"min_timestamp.timestamp": time.Unix(50, 0).UTC(),
		"max_timestamp.timestamp": time.Unix(50, 0).UTC(),
		"row_count.int64":         int64(1),
		"uncompressed_size.int64": int64(len(line4)),
	}, actual[0])

	require.Equal(t, arrowtest.Row{
		"object_path.utf8":        "test/path/obj1",
		"section_index.int64":     int64(0),
		"sort_schema.utf8":        "service_name",
		"service_name.label.utf8": "svcA",
		"min_timestamp.timestamp": ts1,
		"max_timestamp.timestamp": ts2,
		"row_count.int64":         int64(2),
		"uncompressed_size.int64": int64(len(line1) + len(line2)),
	}, actual[1])

	require.Equal(t, arrowtest.Row{
		"object_path.utf8":        "test/path/obj1",
		"section_index.int64":     int64(0),
		"sort_schema.utf8":        "service_name",
		"service_name.label.utf8": "svcB",
		"min_timestamp.timestamp": ts3,
		"max_timestamp.timestamp": ts3,
		"row_count.int64":         int64(1),
		"uncompressed_size.int64": int64(len(line3)),
	}, actual[2])
}

func TestStatsCalculation_MetadataFields(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	ts := time.Unix(500, 0).UTC()
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts, Line: []byte("msg")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	actual := flushAndReadAllStatsTable(t, builder)
	require.Len(t, actual, 1)

	// Verify metadata fields are set correctly.
	require.Equal(t, arrowtest.Row{
		"object_path.utf8":        "test/path/obj1",
		"section_index.int64":     int64(0),
		"sort_schema.utf8":        "service_name",
		"service_name.label.utf8": "svcA",
		"min_timestamp.timestamp": ts,
		"max_timestamp.timestamp": ts,
		"row_count.int64":         int64(1),
		"uncompressed_size.int64": int64(len([]byte("msg"))),
	}, actual[0])
}

func TestStatsCalculation_MissingServiceName(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	batch := []logs.Record{
		{StreamID: 3, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("no svc")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	actual := flushAndReadAllStatsTable(t, builder)
	require.Len(t, actual, 1)
	require.Equal(t, "", actual[0]["service_name.label.utf8"])
}

func TestStatsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

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

	actual := flushAndReadAllStatsTable(t, builder)
	// svcA and svcB
	require.Len(t, actual, 2)

	// Rows are sorted by label value asc, so "svcA" < "svcB" → svcA is actual[0]
	svcA := actual[0]
	require.Equal(t, int64(2), svcA["row_count.int64"])
	require.Equal(t, time.Unix(10, 0).UTC(), svcA["min_timestamp.timestamp"])
	require.Equal(t, time.Unix(30, 0).UTC(), svcA["max_timestamp.timestamp"])
	require.Equal(t, int64(len("first")+len("third batch")), svcA["uncompressed_size.int64"])
}

func TestStatsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, nil))
	require.NoError(t, calc.Flush(context.Background(), ctx))

	// No data → builder is empty, Flush returns ErrBuilderEmpty.
	_, _, err := builder.Flush()
	require.ErrorIs(t, err, indexobj.ErrBuilderEmpty, "expected builder to be empty after empty batch")
}
