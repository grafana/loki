package executor

import (
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/csv"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
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
		// First read should return the first record
		err := pipeline.Read()
		require.NoError(t, err)

		batch, err := pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, record1.NumRows(), batch.NumRows())
		require.Equal(t, schema, batch.Schema())

		// Second read should return the second record
		err = pipeline.Read()
		require.NoError(t, err)

		batch, err = pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, record2.NumRows(), batch.NumRows())
		require.Equal(t, schema, batch.Schema())

		// Third read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)

		_, err = pipeline.Value()
		require.Equal(t, EOF, err)
	})
}
