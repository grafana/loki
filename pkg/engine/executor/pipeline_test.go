package executor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// CSVToArrow converts a CSV string to an Arrow record based on the provided schema.
// It supports int64, float64, and boolean types for now.
func CSVToArrow(fields []arrow.Field, csvData string) (arrow.Record, error) {
	// Create schema
	schema := arrow.NewSchema(fields, nil)

	// Prepare memory allocator
	mem := memory.NewGoAllocator()

	// Create record builder
	builder := array.NewRecordBuilder(mem, schema)
	defer builder.Release()

	// Parse CSV data
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	if len(lines) == 0 {
		return nil, errors.New("empty CSV data")
	}

	// Process each line
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Split into values
		values := strings.Split(line, ",")
		if len(values) != len(fields) {
			return nil, fmt.Errorf("CSV line has %d values, but schema has %d fields", len(values), len(fields))
		}

		// Add each value to its respective builder
		for i, value := range values {
			value = strings.TrimSpace(value)
			fieldBuilder := builder.Field(i)

			// Skip nulls
			if value == "" || value == "null" || value == "NULL" {
				fieldBuilder.AppendNull()
				continue
			}

			// Handle different types
			switch fields[i].Type.ID() {
			case arrow.INT64:
				intVal, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse int64 value '%s': %w", value, err)
				}
				fieldBuilder.(*array.Int64Builder).Append(intVal)

			case arrow.FLOAT64:
				floatVal, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse float64 value '%s': %w", value, err)
				}
				fieldBuilder.(*array.Float64Builder).Append(floatVal)

			case arrow.BOOL:
				boolVal, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("failed to parse bool value '%s': %w", value, err)
				}
				fieldBuilder.(*array.BooleanBuilder).Append(boolVal)

			case arrow.INT32:
				intVal, err := strconv.ParseInt(value, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("failed to parse int32 value '%s': %w", value, err)
				}
				fieldBuilder.(*array.Int32Builder).Append(int32(intVal))

			case arrow.FLOAT32:
				floatVal, err := strconv.ParseFloat(value, 32)
				if err != nil {
					return nil, fmt.Errorf("failed to parse float32 value '%s': %w", value, err)
				}
				fieldBuilder.(*array.Float32Builder).Append(float32(floatVal))

			default:
				return nil, fmt.Errorf("unsupported field type for field '%s': %s", fields[i].Name, fields[i].Type)
			}
		}
	}

	// Build and return the record
	record := builder.NewRecord()
	return record, nil
}

// CSVPipeline implements the Pipeline interface and provides data from a CSV string.
// It divides the data into batches of a specified size.
type CSVPipeline struct {
	fields []arrow.Field
	schema *arrow.Schema

	// Pre-built batches
	batches []arrow.Record

	// Current state
	currentBatch int
	currentErr   error
}

// NewCSVPipeline creates a new Pipeline from CSV data with a specified batch size.
// It pre-builds all batches during initialization to ensure schema compatibility.
func NewCSVPipeline(batchSize int, fields []arrow.Field, csvData string) (*CSVPipeline, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("batch size must be positive, got %d", batchSize)
	}

	schema := arrow.NewSchema(fields, nil)

	// Split CSV into lines
	lines := strings.Split(strings.TrimSpace(csvData), "\n")
	validLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			validLines = append(validLines, line)
		}
	}

	totalRows := len(validLines)
	batchCount := (totalRows + batchSize - 1) / batchSize // Ceiling division

	// Pre-build all batches
	batches := make([]arrow.Record, 0, batchCount)

	// Early return for empty CSV
	if totalRows == 0 {
		return &CSVPipeline{
			fields:  fields,
			schema:  schema,
			batches: batches,
		}, nil
	}

	// Create all batches
	for i := 0; i < batchCount; i++ {
		start := i * batchSize
		end := min(start+batchSize, totalRows)

		// Create a string with just the lines for this batch
		batchCSV := strings.Join(validLines[start:end], "\n")

		// Create arrow record from the batch
		rec, err := CSVToArrow(fields, batchCSV)
		if err != nil {
			// Clean up any previously created batches
			for _, batch := range batches {
				batch.Release()
			}
			return nil, fmt.Errorf("error creating batch %d: %w", i, err)
		}

		batches = append(batches, rec)
	}

	return &CSVPipeline{
		fields:  fields,
		schema:  schema,
		batches: batches,
	}, nil
}

// Read implements Pipeline.Read
func (p *CSVPipeline) Read() error {
	// If we've already reached EOF or another error occurred, return it
	if p.currentErr != nil {
		return p.currentErr
	}

	// Check if we've processed all batches
	if p.currentBatch >= len(p.batches) {
		p.currentErr = EOF
		return p.currentErr
	}

	// Move to the next batch
	p.currentBatch++

	return nil
}

// Value implements Pipeline.Value
func (p *CSVPipeline) Value() (arrow.Record, error) {
	if p.currentErr != nil {
		return nil, p.currentErr
	}

	if p.currentBatch == 0 || p.currentBatch > len(p.batches) {
		return nil, fmt.Errorf("no current batch - call Read() first")
	}

	return p.batches[p.currentBatch-1], nil
}

// Close implements Pipeline.Close
func (p *CSVPipeline) Close() {
	for _, batch := range p.batches {
		batch.Release()
	}
	p.batches = nil
}

// Inputs implements Pipeline.Inputs
func (p *CSVPipeline) Inputs() []Pipeline {
	return nil
}

// Transport implements Pipeline.Transport
func (p *CSVPipeline) Transport() Transport {
	return Local
}

func TestCSVPipeline(t *testing.T) {
	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "value", Type: arrow.PrimitiveTypes.Int64},
	}

	csvData := `1,10
2,20
3,30
4,40
5,50
6,60
7,70
8,80
9,90
10,100`

	t.Run("batch_size_equal_to_total", func(t *testing.T) {
		pipeline, err := NewCSVPipeline(10, fields, csvData)
		require.NoError(t, err)
		defer pipeline.Close()

		// Should be only one batch
		require.Equal(t, 1, len(pipeline.batches))

		// Read the batch
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err := pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(10), rec.NumRows())

		// Next read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)
	})

	t.Run("batch_size_less_than_total", func(t *testing.T) {
		pipeline, err := NewCSVPipeline(3, fields, csvData)
		require.NoError(t, err)
		defer pipeline.Close()

		// Should be 4 batches (ceil(10/3))
		require.Equal(t, 4, len(pipeline.batches))

		// Read first batch (rows 1-3)
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err := pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(3), rec.NumRows())
		idCol := rec.Column(0).(*array.Int64)
		require.Equal(t, int64(1), idCol.Value(0))
		require.Equal(t, int64(2), idCol.Value(1))
		require.Equal(t, int64(3), idCol.Value(2))

		// Read second batch (rows 4-6)
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err = pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(3), rec.NumRows())
		idCol = rec.Column(0).(*array.Int64)
		require.Equal(t, int64(4), idCol.Value(0))
		require.Equal(t, int64(5), idCol.Value(1))
		require.Equal(t, int64(6), idCol.Value(2))

		// Read third batch (rows 7-9)
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err = pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(3), rec.NumRows())
		idCol = rec.Column(0).(*array.Int64)
		require.Equal(t, int64(7), idCol.Value(0))
		require.Equal(t, int64(8), idCol.Value(1))
		require.Equal(t, int64(9), idCol.Value(2))

		// Read fourth batch (row 10)
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err = pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(1), rec.NumRows())
		idCol = rec.Column(0).(*array.Int64)
		require.Equal(t, int64(10), idCol.Value(0))

		// Next read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)
	})

	t.Run("batch_size_greater_than_total", func(t *testing.T) {
		pipeline, err := NewCSVPipeline(20, fields, csvData)
		require.NoError(t, err)
		defer pipeline.Close()

		// Should be only one batch
		require.Equal(t, 1, len(pipeline.batches))

		// Read the batch
		err = pipeline.Read()
		require.NoError(t, err)

		rec, err := pipeline.Value()
		require.NoError(t, err)
		require.Equal(t, int64(10), rec.NumRows())

		// Next read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)
	})

	t.Run("empty_csv", func(t *testing.T) {
		pipeline, err := NewCSVPipeline(10, fields, "")
		require.NoError(t, err)
		defer pipeline.Close()

		// Should be zero batches
		require.Equal(t, 0, len(pipeline.batches))

		// First read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)
	})

	t.Run("invalid_batch_size", func(t *testing.T) {
		_, err := NewCSVPipeline(0, fields, csvData)
		require.Error(t, err)
		require.Contains(t, err.Error(), "batch size must be positive")

		_, err = NewCSVPipeline(-5, fields, csvData)
		require.Error(t, err)
		require.Contains(t, err.Error(), "batch size must be positive")
	})

	t.Run("schema_incompatibility_detected_at_init", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "valid", Type: arrow.FixedWidthTypes.Boolean},
		}

		// CSV with invalid boolean value in the middle
		csvData := `1,true
2,not_a_bool
3,true`

		// Error should be detected during initialization
		_, err := NewCSVPipeline(1, fields, csvData)
		require.Error(t, err)
		require.Contains(t, err.Error(), "error creating batch 1")
		require.Contains(t, err.Error(), "failed to parse bool value")
	})
}

func TestCSVToArrow(t *testing.T) {
	t.Run("int64_and_bool", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "valid", Type: arrow.FixedWidthTypes.Boolean},
		}

		csvData := `0,true
1,false
2,true`

		record, err := CSVToArrow(fields, csvData)
		require.NoError(t, err)
		defer record.Release()

		require.Equal(t, int64(3), record.NumRows())
		require.Equal(t, int64(2), record.NumCols())

		// Check id column (int64)
		idCol := record.Column(0).(*array.Int64)
		require.Equal(t, int64(0), idCol.Value(0))
		require.Equal(t, int64(1), idCol.Value(1))
		require.Equal(t, int64(2), idCol.Value(2))

		// Check valid column (bool)
		validCol := record.Column(1).(*array.Boolean)
		require.Equal(t, true, validCol.Value(0))
		require.Equal(t, false, validCol.Value(1))
		require.Equal(t, true, validCol.Value(2))
	})

	t.Run("with_float", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "score", Type: arrow.PrimitiveTypes.Float64},
		}

		csvData := `1,10.5
2,20.75
3,30.25`

		record, err := CSVToArrow(fields, csvData)
		require.NoError(t, err)
		defer record.Release()

		// Check score column (float64)
		scoreCol := record.Column(1).(*array.Float64)
		require.Equal(t, 10.5, scoreCol.Value(0))
		require.Equal(t, 20.75, scoreCol.Value(1))
		require.Equal(t, 30.25, scoreCol.Value(2))
	})

	t.Run("with_nulls", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "value", Type: arrow.PrimitiveTypes.Int64},
		}

		csvData := `1,10
2,null
3,30`

		record, err := CSVToArrow(fields, csvData)
		require.NoError(t, err)
		defer record.Release()

		// Check value column with nulls
		valueCol := record.Column(1)
		require.Equal(t, 1, valueCol.NullN())
		require.False(t, valueCol.IsNull(0))
		require.True(t, valueCol.IsNull(1))
		require.False(t, valueCol.IsNull(2))
	})

	t.Run("error_mismatch_columns", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "valid", Type: arrow.FixedWidthTypes.Boolean},
		}

		csvData := `0,true
1,false,extra
2,true`

		_, err := CSVToArrow(fields, csvData)
		require.Error(t, err)
		require.Contains(t, err.Error(), "CSV line has 3 values, but schema has 2 fields")
	})

	t.Run("error_invalid_type", func(t *testing.T) {
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "valid", Type: arrow.FixedWidthTypes.Boolean},
		}

		csvData := `0,true
1,not_a_bool
2,true`

		_, err := CSVToArrow(fields, csvData)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse bool value")
	})
}
