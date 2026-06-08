package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// postingsTable holds the result of reading all postings from an object:
// rows has opaque columns stripped; opaque has the raw bytes indexed by
// field name then row index.
type postingsTable struct {
	rows   arrowtest.Rows
	opaque map[string][][]byte // "bloom_filter.binary" and "stream_id_bitmap.binary"
}

// flushAndReadAllPostingsTable flushes the builder and reads all postings
// from all sections, returning stripped rows and opaque column data separately.
func flushAndReadAllPostingsTable(t *testing.T, builder *indexobj.Builder) postingsTable {
	t.Helper()
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	result := postingsTable{
		opaque: map[string][][]byte{
			"bloom_filter.binary":     nil,
			"stream_id_bitmap.binary": nil,
		},
	}

	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(context.Background(), s)
		require.NoError(t, err)

		r := postings.NewReader(postings.ReaderOptions{
			Columns:   sec.Columns(),
			Allocator: memory.DefaultAllocator,
		})
		require.NoError(t, r.Open(context.Background()))
		t.Cleanup(func() { _ = r.Close() })

		tbl, err := readPostingsTable(context.Background(), r)
		if errors.Is(err, io.EOF) {
			continue
		}
		require.NoError(t, err)

		secRows, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
		require.NoError(t, err)

		blooms := extractBinaryColumn(t, tbl, "bloom_filter.binary")
		bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
		result.opaque["bloom_filter.binary"] = append(result.opaque["bloom_filter.binary"], blooms...)
		result.opaque["stream_id_bitmap.binary"] = append(result.opaque["stream_id_bitmap.binary"], bitmaps...)
		result.rows = append(result.rows, stripOpaqueCols(secRows, "bloom_filter.binary", "stream_id_bitmap.binary")...)
	}
	return result
}

// findRow returns the index of the first row where every (k,v) in match is
// satisfied using == on the values arrowtest.TableRows produces. Returns -1
// if not found.
//
// Match values must be comparable types (int64, string, time.Time). Do NOT
// pass []byte values — interface == comparison on []byte panics at runtime.
func findRow(rows arrowtest.Rows, match map[string]any) int {
	for i, row := range rows {
		ok := true
		for k, v := range match {
			if row[k] != v {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// readPostingsTable drains a postings.Reader into an arrow.Table.
func readPostingsTable(ctx context.Context, r *postings.Reader) (arrow.Table, error) {
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

// stripOpaqueCols returns a copy of rows with the named keys removed.
func stripOpaqueCols(rows arrowtest.Rows, keys ...string) arrowtest.Rows {
	out := make(arrowtest.Rows, len(rows))
	for i, row := range rows {
		cp := make(arrowtest.Row, len(row))
		maps.Copy(cp, row)
		for _, k := range keys {
			delete(cp, k)
		}
		out[i] = cp
	}
	return out
}

// extractBinaryColumn extracts a named Binary arrow column as [][]byte.
func extractBinaryColumn(t *testing.T, table arrow.Table, field string) [][]byte {
	t.Helper()
	idx := table.Schema().FieldIndices(field)
	require.Len(t, idx, 1, "field %q not found in schema", field)
	col := table.Column(idx[0])
	var out [][]byte
	for _, chunk := range col.Data().Chunks() {
		bin, ok := chunk.(*array.Binary)
		require.True(t, ok, "field %q is not a Binary column", field)
		for i := 0; i < bin.Len(); i++ {
			if bin.IsNull(i) {
				out = append(out, nil)
				continue
			}
			src := bin.Value(i)
			cp := make([]byte, len(src))
			copy(cp, src)
			out = append(out, cp)
		}
	}
	return out
}

func TestLabelPostingsCalculation_BasicPostings(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	// Stream 1: service_name=svcA, env=prod
	// Stream 2: service_name=svcB, env=dev
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")}, // svcA, prod
		{StreamID: 2, Timestamp: ts2, Line: []byte("world")}, // svcB, dev
		{StreamID: 1, Timestamp: ts3, Line: []byte("again")}, // svcA, prod again
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	tbl := flushAndReadAllPostingsTable(t, builder)

	// We expect 4 label postings: service_name=svcA, service_name=svcB, env=prod, env=dev.
	require.Len(t, tbl.rows, 4)

	// All should be label-kind.
	for _, row := range tbl.rows {
		require.Equal(t, int64(postings.KindLabel), row["kind.int64"])
		require.NotNil(t, row["label_value.utf8"])
	}

	// Find svcA and svcB.
	i := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcA",
	})
	require.NotEqual(t, -1, i, "expected svcA posting")
	svcARow := tbl.rows[i]

	j := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcB",
	})
	require.NotEqual(t, -1, j, "expected svcB posting")

	// svcA should have bit 1 set.
	require.NotEmpty(t, tbl.opaque["stream_id_bitmap.binary"][i])
	// svcB should have bit 2 set.
	require.NotEmpty(t, tbl.opaque["stream_id_bitmap.binary"][j])

	// Verify timestamps for svcA (min=ts1, max=ts3).
	require.Equal(t, ts1.UTC(), svcARow["min_timestamp.timestamp"])
	require.Equal(t, ts3.UTC(), svcARow["max_timestamp.timestamp"])

	// Verify env labels are also present.
	k := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "env",
		"label_value.utf8": "prod",
	})
	require.NotEqual(t, -1, k, "expected env=prod posting")

	l := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "env",
		"label_value.utf8": "dev",
	})
	require.NotEqual(t, -1, l, "expected env=dev posting")
}

func TestLabelPostingsCalculation_BitmapsNormalized(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	// Stream 1 = svcA/prod, Stream 2 = svcB/dev (from makeTestStreamLabels).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")}, // svcA, prod
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("b")}, // svcB, dev
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	tbl := flushAndReadAllPostingsTable(t, builder)
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, tbl.rows, 4)

	i := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcA",
	})
	require.NotEqual(t, -1, i)

	j := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcB",
	})
	require.NotEqual(t, -1, j)

	// All bitmaps should be the same length after normalization.
	for k := 1; k < len(tbl.opaque["stream_id_bitmap.binary"]); k++ {
		require.Equal(t, len(tbl.opaque["stream_id_bitmap.binary"][0]), len(tbl.opaque["stream_id_bitmap.binary"][k]),
			"all bitmaps should be normalized to the same size")
	}

	// svcA posting should include stream 1 but not stream 2.
	require.True(t, isBitSet(tbl.opaque["stream_id_bitmap.binary"][i], 1), "svcA bitmap should have bit 1 set")
	require.False(t, isBitSet(tbl.opaque["stream_id_bitmap.binary"][i], 2), "svcA bitmap should not have bit 2 set")

	// svcB posting should include stream 2 but not stream 1.
	require.True(t, isBitSet(tbl.opaque["stream_id_bitmap.binary"][j], 2), "svcB bitmap should have bit 2 set")
	require.False(t, isBitSet(tbl.opaque["stream_id_bitmap.binary"][j], 1), "svcB bitmap should not have bit 1 set")
}

// isBitSet checks if bit n is set in a LSB-numbered bitmap (Arrow convention).
func isBitSet(bitmap []byte, n int) bool {
	if n/8 >= len(bitmap) {
		return false
	}
	return bitmap[n/8]&(1<<(n%8)) != 0
}

func TestLabelPostingsCalculation_TimestampsAndSizes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(30, 0).UTC()

	// All records are stream 1 (service_name=svcA, env=prod).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")},  // svcA, ts=10
		{StreamID: 1, Timestamp: ts2, Line: []byte("world!")}, // svcA, ts=20
		{StreamID: 1, Timestamp: ts3, Line: []byte("mid")},    // svcA, ts=30
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	tbl := flushAndReadAllPostingsTable(t, builder)
	// 2 postings: service_name=svcA, env=prod (both from stream 1).
	require.Len(t, tbl.rows, 2)

	// Check timestamps and sizes on the service_name=svcA posting.
	i := findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcA",
	})
	require.NotEqual(t, -1, i)
	row := tbl.rows[i]
	require.Equal(t, ts1.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts3.UTC(), row["max_timestamp.timestamp"])
	expectedSize := int64(len("hello") + len("world!") + len("mid"))
	require.Equal(t, expectedSize, row["uncompressed_size.int64"])
}

func TestLabelPostingsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	// No data → builder is empty, Flush returns ErrBuilderEmpty.
	_, _, err := builder.Flush()
	require.ErrorIs(t, err, indexobj.ErrBuilderEmpty, "expected builder to be empty after empty batch")
}

func TestLabelPostingsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")},
	}
	batch2 := []logs.Record{
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("bb")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch1))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch2))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	tbl := flushAndReadAllPostingsTable(t, builder)
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, tbl.rows, 4)

	// Verify that both svcA and svcB postings are present.
	require.NotEqual(t, -1, findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcA",
	}))
	require.NotEqual(t, -1, findRow(tbl.rows, map[string]any{
		"column_name.utf8": "service_name",
		"label_value.utf8": "svcB",
	}))
	// Verify env labels are also present.
	require.NotEqual(t, -1, findRow(tbl.rows, map[string]any{
		"column_name.utf8": "env",
		"label_value.utf8": "prod",
	}))
	require.NotEqual(t, -1, findRow(tbl.rows, map[string]any{
		"column_name.utf8": "env",
		"label_value.utf8": "dev",
	}))
}

// BenchmarkLabelPostingsCalculation_ProcessBatch benchmarks ProcessBatch with
// varying numbers of log records and distinct streams. This helps track
// performance since the calculation must iterate through all rows in a logs
// section.
func BenchmarkLabelPostingsCalculation_ProcessBatch(b *testing.B) {
	benchCases := []struct {
		records int
		streams int
	}{
		{records: 1_000, streams: 10},
		{records: 10_000, streams: 10},
		{records: 10_000, streams: 100},
		{records: 100_000, streams: 10},
		{records: 100_000, streams: 100},
		{records: 100_000, streams: 1_000},
	}

	for _, bc := range benchCases {
		b.Run(fmt.Sprintf("records=%d/streams=%d", bc.records, bc.streams), func(b *testing.B) {
			// Build stream labels map with multiple labels per stream.
			streamLabels := make(map[int64]labels.Labels, bc.streams)
			for i := range bc.streams {
				streamLabels[int64(i)] = labels.FromStrings(
					"service_name", fmt.Sprintf("svc-%d", i),
					"cluster", fmt.Sprintf("cluster-%d", i%5),
					"namespace", fmt.Sprintf("ns-%d", i%10),
				)
			}

			// Pre-build the batch: records are distributed round-robin across streams.
			batch := make([]logs.Record, bc.records)
			for i := range batch {
				batch[i] = logs.Record{
					StreamID:  int64(i % bc.streams),
					Timestamp: time.Unix(int64(i), 0).UTC(),
					Line:      []byte("log line payload for benchmarking"),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				builder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
				if err != nil {
					b.Fatal(err)
				}
				calcCtx := &logsCalculationContext{
					tenantID:     "bench-tenant",
					objectPath:   "bench/path",
					sectionIdx:   0,
					streamLabels: streamLabels,
					builder:      builder,
				}
				calc := &labelPostingsCalculation{}

				if err := calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}); err != nil {
					b.Fatal(err)
				}
				if err := calc.ProcessBatch(context.Background(), calcCtx, batch); err != nil {
					b.Fatal(err)
				}
				if err := calc.Flush(context.Background(), calcCtx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
