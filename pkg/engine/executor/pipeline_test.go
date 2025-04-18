package executor

import (
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/csv"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// CSVToArrow converts a CSV string to an Arrow record based on the provided schema.
// It uses the Arrow CSV reader for parsing.
func CSVToArrow(fields []arrow.Field, csvData string) (arrow.Record, error) {
	return CSVToArrowWithAllocator(memory.NewGoAllocator(), fields, csvData)
}

// CSVToArrowWithAllocator converts a CSV string to an Arrow record based on the provided schema
// using the specified memory allocator. It reads all rows from the CSV into a single record.
func CSVToArrowWithAllocator(allocator memory.Allocator, fields []arrow.Field, csvData string) (arrow.Record, error) {
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
	defer reader.Release()

	if !reader.Next() {
		return nil, errors.New("failed to read CSV data")
	}

	return reader.Record(), nil
}

// BufferedPipeline is a pipeline implementation that reads from a fixed set of Arrow records.
// It implements the Pipeline interface and serves as a simple source for testing and data injection.
type BufferedPipeline struct {
	records []arrow.Record
	current int
	state   state
}

// NewBufferedPipeline creates a new BufferedPipeline from a set of Arrow records.
// The pipeline will return these records in sequence.
func NewBufferedPipeline(records ...arrow.Record) *BufferedPipeline {
	for _, rec := range records {
		if rec != nil {
			rec.Retain()
		}
	}

	return &BufferedPipeline{
		records: records,
		current: -1, // Start before the first record
		state:   failureState(EOF),
	}
}

// Read implements Pipeline.
// It advances to the next record and returns EOF when all records have been read.
func (p *BufferedPipeline) Read() error {
	// Release previous record if it exists
	if p.state.batch != nil {
		p.state.batch.Release()
	}

	p.current++
	if p.current >= len(p.records) {
		p.state = failureState(EOF)
		return EOF
	}

	p.state = successState(p.records[p.current])
	return nil
}

// Value implements Pipeline.
// It returns the current record and error state.
func (p *BufferedPipeline) Value() (arrow.Record, error) {
	return p.state.Value()
}

// Close implements Pipeline.
// It releases all records being held.
func (p *BufferedPipeline) Close() {
	for _, rec := range p.records {
		if rec != nil {
			rec.Release()
		}
	}

	if p.state.batch != nil {
		p.state.batch.Release()
		p.state.batch = nil
	}

	p.records = nil
}

// Inputs implements Pipeline.
// CSV pipeline is a source, so it has no inputs.
func (p *BufferedPipeline) Inputs() []Pipeline {
	return nil
}

// Transport implements Pipeline.
// CSVPipeline is always considered a Local transport.
func (p *BufferedPipeline) Transport() Transport {
	return Local
}

func TestCSVPipeline(t *testing.T) {
	// Define test schema
	fields := []arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "age", Type: arrow.PrimitiveTypes.Int32},
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
