package pointers_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
)

const maxStreamID = 200

// buildBenchSection creates a section with many pointers for benchmarking
func buildBenchSection(b *testing.B, numPointers int) *pointers.Section {
	b.Helper()

	sectionBuilder := pointers.NewBuilder(nil, 0, 2)

	// Create diverse set of pointers with different stream IDs
	for i := 0; i < numPointers; i++ {
		streamID := int64(i % maxStreamID) // Cycle through different stream IDs
		path := "path/to/object"
		section := int64(i % 10)
		startTs := unixTime(int64(i * 100))
		endTs := unixTime(int64(i*100 + 50))

		sectionBuilder.ObserveStream(path, section, streamID, streamID, startTs, 1024)
		sectionBuilder.ObserveStream(path, section, streamID, streamID, endTs, 0)
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { closer.Close() })

	sec, err := pointers.Open(context.Background(), obj.Sections()[0])
	require.NoError(b, err)
	return sec
}

type readerBenchParams struct {
	name         string
	numPointers  int
	numStreamIDs int
}

func BenchmarkReaders(b *testing.B) {
	benchmarks := []readerBenchParams{
		{
			name:         "1k pointers, 10 stream IDs",
			numPointers:  1000,
			numStreamIDs: 10,
		},
		{
			name:         "1k pointers, 200 stream IDs",
			numPointers:  1000,
			numStreamIDs: 200,
		},
		{
			name:         "10k pointers, 200 stream IDs",
			numPointers:  10000,
			numStreamIDs: 200,
		},
		{
			name:         "100k pointers, 200 stream IDs",
			numPointers:  100000,
			numStreamIDs: 200,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.Run("RowReader", func(b *testing.B) {
				benchmarkRowReader(b, bm)
			})
			b.Run("Reader", func(b *testing.B) {
				benchmarkReader(b, bm)
			})
		})
	}
}

func benchmarkRowReader(b *testing.B, params readerBenchParams) {
	ctx := context.Background()
	sec := buildBenchSection(b, params.numPointers)

	// Prepare stream IDs to match
	streamIDs := make([]int64, params.numStreamIDs)
	for i := 0; i < params.numStreamIDs; i++ {
		streamIDs[i] = int64(i)
	}

	// Prepare predicate if needed
	predicate := pointers.TimeRangeRowPredicate{
		Start: unixTime(0),
		End:   unixTime(100000),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		reader := pointers.NewRowReader(sec)
		err := reader.MatchStreams(slices.Values(streamIDs))
		require.NoError(b, err)

		err = reader.SetPredicate(predicate)
		require.NoError(b, err)

		buf := make([]pointers.SectionPointer, 128)
		totalRead := 0

		for {
			n, err := reader.Read(ctx, buf)
			totalRead += n

			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(b, err)
			}
		}

		require.NoError(b, reader.Close())

		// Ensure we actually read something
		if totalRead == 0 {
			b.Fatal("read 0 pointers")
		}
	}

	b.StopTimer()
}

func benchmarkReader(b *testing.B, params readerBenchParams) {
	ctx := context.Background()
	sec := buildBenchSection(b, params.numPointers)

	// Get all columns for reading
	columns := sec.Columns()

	// Prepare stream IDs to match
	streamIDs := make([]scalar.Scalar, params.numStreamIDs)
	for i := 0; i < params.numStreamIDs; i++ {
		streamIDs[i] = scalar.NewInt64Scalar(int64(i))
	}

	var streamIDCol *pointers.Column
	for _, col := range columns {
		if col.Type == pointers.ColumnTypeStreamID {
			streamIDCol = col
			break
		}
	}
	require.NotNil(b, streamIDCol)

	// Build predicates
	var predicates []pointers.Predicate

	predicates = append(predicates, pointers.InPredicate{
		Column: streamIDCol,
		Values: streamIDs,
	})

	var minTsCol, maxTsCol *pointers.Column
	for _, col := range columns {
		switch col.Type {
		case pointers.ColumnTypeMinTimestamp:
			minTsCol = col
		case pointers.ColumnTypeMaxTimestamp:
			maxTsCol = col
		}
	}
	require.NotNil(b, minTsCol)
	require.NotNil(b, maxTsCol)

	startScalar := scalar.NewTimestampScalar(arrow.Timestamp(0), arrow.FixedWidthTypes.Timestamp_ns)
	endScalar := scalar.NewTimestampScalar(arrow.Timestamp(100000000000000), arrow.FixedWidthTypes.Timestamp_ns)

	predicates = append(predicates, pointers.AndPredicate{
		Left: pointers.GreaterThanPredicate{
			Column: maxTsCol,
			Value:  startScalar,
		},
		Right: pointers.LessThanPredicate{
			Column: minTsCol,
			Value:  endScalar,
		},
	})

	b.ResetTimer()
	b.ReportAllocs()

	opts := pointers.ReaderOptions{
		Columns:    columns,
		Predicates: predicates,
	}

	reader := pointers.NewReader(opts)

	for range b.N {
		reader.Reset(opts)
		totalRows := int64(0)

		for {
			rec, err := reader.Read(ctx, 128)
			if rec != nil {
				totalRows += rec.NumRows()
				rec.Release()
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(b, err)
			}
		}

		require.NoError(b, reader.Close())

		// Ensure we actually read something
		if totalRows == 0 {
			b.Fatal("read 0 rows")
		}
	}

	b.StopTimer()
}
