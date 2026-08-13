package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// These parameters model a broad-selector metric query: a stream_id IN (set)
// predicate AND a timestamp range predicate over a logs section sorted by
// [stream_id ASC, timestamp]. Every row passes both predicates, so the filter
// loop in readAndFilterPrimaryColumns runs over every row.
const (
	benchFilterRows    = 200_000
	benchFilterStreams = 200
)

func buildFilterBenchDataset(b *testing.B) (Dataset, []Column) {
	b.Helper()

	rowsPerStream := benchFilterRows / benchFilterStreams
	streamIDCol := buildInt64BenchColumn(b, "stream_id", func(row int) int64 {
		return int64(row / rowsPerStream)
	})
	tsCol := buildInt64BenchColumn(b, "timestamp", func(row int) int64 {
		return int64(row)
	})

	dset := FromMemory([]*MemColumn{streamIDCol, tsCol})
	cols, err := result.Collect(dset.ListColumns(context.Background()))
	if err != nil {
		b.Fatal(err)
	}
	return dset, cols
}

func buildInt64BenchColumn(b *testing.B, logical string, value func(row int) int64) *MemColumn {
	b.Helper()

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 1024 * 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: logical},
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics:   StatisticsOptions{StoreRangeStats: true},
	})
	if err != nil {
		b.Fatal(err)
	}
	for row := range benchFilterRows {
		if err := builder.Append(row, Int64Value(value(row))); err != nil {
			b.Fatal(err)
		}
	}
	col, err := builder.Flush()
	if err != nil {
		b.Fatal(err)
	}
	return col
}

// BenchmarkRowReaderReadAndFilter measures the per-row predicate evaluation path
// (readAndFilterPrimaryColumns -> checkPredicate) for a broad-selector metric
// query. Both columns are primary (used in a predicate), so there is no
// secondary fill; the benchmark isolates decode + predicate evaluation.
func BenchmarkRowReaderReadAndFilter(b *testing.B) {
	dset, columns := buildFilterBenchDataset(b)
	streamIDCol, tsCol := columns[0], columns[1]

	ids := make([]Value, 0, benchFilterStreams)
	for i := range benchFilterStreams {
		ids = append(ids, Int64Value(int64(i)))
	}

	predicates := []Predicate{
		InPredicate{Column: streamIDCol, Values: NewMemoizedInt64ValueSet(ids)},
		AndPredicate{
			Left:  GreaterThanPredicate{Column: tsCol, Value: Int64Value(-1)},
			Right: LessThanPredicate{Column: tsCol, Value: Int64Value(benchFilterRows)},
		},
	}

	opts := RowReaderOptions{
		Dataset:    dset,
		Columns:    columns,
		Predicates: predicates,
		Prefetch:   true,
	}

	ctx := context.Background()
	batch := make([]Row, 1024)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		r := NewRowReader(opts)
		if err := r.Open(ctx); err != nil {
			b.Fatal(err)
		}

		var total int
		for {
			clear(batch)
			n, err := r.Read(ctx, batch)
			total += n
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}

		_ = r.Close()
		if total != benchFilterRows {
			b.Fatalf("expected %d rows, got %d", benchFilterRows, total)
		}
	}
}
