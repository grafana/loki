package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/csv"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// CSVToArrow converts a CSV string to an Arrow record based on the provided schema.
// It uses the Arrow CSV reader for parsing.
func CSVToArrow(fields []arrow.Field, csvData string) (arrow.Record, error) {
	return CSVToArrowWithAllocator(memory.NewGoAllocator(), fields, csvData)
}

// CSVToArrowWithAllocator converts a CSV string to an Arrow record based on the provided schema
// using the specified memory allocator. It reads all rows from the CSV into a single record.
func CSVToArrowWithAllocator(allocator memory.Allocator, fields []arrow.Field, csvData string) (arrow.Record, error) {
	// first, trim the csvData to remove any preceding and trailing whitespace/line breaks
	csvData = strings.TrimSpace(csvData)

	// Create schema
	schema := arrow.NewSchema(fields, nil)

	// Set up CSV reader with stringified data
	input := strings.NewReader(csvData)
	reader := csv.NewReader(
		input,
		schema,
		csv.WithAllocator(allocator),
		csv.WithNullReader(true),
		csv.WithComma(','),
		csv.WithChunk(-1), // Read all rows
	)

	if !reader.Next() {
		return nil, errors.New("failed to read CSV data")
	}

	return reader.Record(), nil
}

func TestCSVPipeline(t *testing.T) {
	// Define test schema
	fields := []arrow.Field{
		{Name: "name", Type: datatype.Arrow.String},
		{Name: "age", Type: datatype.Arrow.Integer},
	}
	schema := arrow.NewSchema(fields, nil)

	// Create test data
	csvData1 := "Alice,30\nBob,25"
	csvData2 := "Charlie,35\nDave,40"

	// Convert to Arrow records
	record1, err := CSVToArrow(fields, csvData1)
	require.NoError(t, err)
	defer record1.Release()

	record2, err := CSVToArrow(fields, csvData2)
	require.NoError(t, err)
	defer record2.Release()

	// Create a CSVPipeline with the test records
	pipeline := NewBufferedPipeline(record1, record2)
	defer pipeline.Close()

	// Test pipeline behavior
	t.Run("should have correct transport type", func(t *testing.T) {
		require.Equal(t, Local, pipeline.Transport())
	})

	t.Run("should have no inputs", func(t *testing.T) {
		require.Empty(t, pipeline.Inputs())
	})

	t.Run("should return records in order", func(t *testing.T) {
		ctx := t.Context()

		// First read should return the first record
		err := pipeline.Read(ctx)
		require.NoError(t, err)

		batch, err := pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, record1.NumRows(), batch.NumRows())
		require.Equal(t, schema, batch.Schema())

		// Second read should return the second record
		err = pipeline.Read(ctx)
		require.NoError(t, err)

		batch, err = pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, record2.NumRows(), batch.NumRows())
		require.Equal(t, schema, batch.Schema())

		// Third read should return EOF
		err = pipeline.Read(ctx)
		require.Equal(t, EOF, err)

		_, err = pipeline.Value()
		require.Equal(t, EOF, err)
	})
}

func newInstrumentedPipeline(inner Pipeline) *instrumentedPipeline {
	return &instrumentedPipeline{
		inner:     inner,
		callCount: make(map[string]int),
	}
}

type instrumentedPipeline struct {
	inner     Pipeline
	callCount map[string]int
}

// Inputs implements Pipeline.
func (i *instrumentedPipeline) Inputs() []Pipeline {
	i.callCount["Inputs"]++
	return i.inner.Inputs()
}

// Close implements Pipeline.
func (i *instrumentedPipeline) Close() {
	i.callCount["Close"]++
	i.inner.Close()
}

// Read implements Pipeline.
func (i *instrumentedPipeline) Read(ctx context.Context) error {
	i.callCount["Read"]++
	return i.inner.Read(ctx)
}

// Transport implements Pipeline.
func (i *instrumentedPipeline) Transport() Transport {
	i.callCount["Transport"]++
	return i.inner.Transport()
}

// Value implements Pipeline.
func (i *instrumentedPipeline) Value() (arrow.Record, error) {
	i.callCount["Value"]++
	return i.inner.Value()
}

var _ Pipeline = (*instrumentedPipeline)(nil)

func Test_prefetchWrapper_Read(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())

	batch1 := arrowtest.Rows{
		{"message": "log line 1"},
		{"message": "log line 2"},
	}
	batch2 := arrowtest.Rows{
		{"message": "log line 3"},
		{"message": "log line 4"},
	}
	batch3 := arrowtest.Rows{
		{"message": "log line 5"},
	}

	pipeline := NewBufferedPipeline(
		batch1.Record(alloc, batch1.Schema()),
		batch2.Record(alloc, batch2.Schema()),
		batch3.Record(alloc, batch3.Schema()),
	)

	instrumentedPipeline := newInstrumentedPipeline(pipeline)
	prefetchingPipeline := newPrefetchingPipeline(instrumentedPipeline)

	require.Equal(t, 0, instrumentedPipeline.callCount["Read"])

	ctx := t.Context()

	// Read first batch
	err := prefetchingPipeline.Read(ctx)
	require.NoError(t, err)
	v, _ := prefetchingPipeline.Value()
	require.Equal(t, int64(2), v.NumRows())

	time.Sleep(10 * time.Millisecond)                           // ensure that next batch has been prefetched
	require.Equal(t, 2, instrumentedPipeline.callCount["Read"]) // 1 record consumed + 1 record pre-fetched

	// Read second batch
	err = prefetchingPipeline.Read(ctx)
	require.NoError(t, err)
	v, _ = prefetchingPipeline.Value()
	require.Equal(t, int64(2), v.NumRows())

	time.Sleep(10 * time.Millisecond)                           // ensure that next batch has been prefetched
	require.Equal(t, 3, instrumentedPipeline.callCount["Read"]) // 2 records consumed + 1 record pre-fetched

	// Read third/last batch
	err = prefetchingPipeline.Read(ctx)
	require.NoError(t, err)
	v, _ = prefetchingPipeline.Value()
	require.Equal(t, int64(1), v.NumRows())

	time.Sleep(10 * time.Millisecond)                           // ensure that next batch has been prefetched
	require.Equal(t, 4, instrumentedPipeline.callCount["Read"]) // 3 records consumed + 1 EOF pre-fetched

	// Read EOF
	err = prefetchingPipeline.Read(ctx)
	require.ErrorContains(t, err, EOF.Error())

	time.Sleep(10 * time.Millisecond)                           // ensure that next batch has been prefetched
	require.Equal(t, 4, instrumentedPipeline.callCount["Read"]) // 3 records + 1 EOF consumed

	// Assert that records are released
	prefetchingPipeline.Close()
	alloc.AssertSize(t, 0)
}

func Test_prefetchWrapper_Close(t *testing.T) {
	t.Run("initialized prefetcher", func(t *testing.T) {
		w := newPrefetchingPipeline(emptyPipeline())
		require.ErrorIs(t, EOF, w.Read(t.Context()))
		require.NotPanics(t, w.Close)
	})

	t.Run("uninitialized prefetcher", func(t *testing.T) {
		w := newPrefetchingPipeline(emptyPipeline())
		require.NotPanics(t, w.Close)
	})
}
