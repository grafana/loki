package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func equalPipelines(t *testing.T, p1, p2 Pipeline) {
	// Track batch position for each pipeline
	var (
		batch1, batch2 arrow.Record
		pos1, pos2     int64
		totalRows      int64
	)

	// Read initial batches
	err1 := p1.Read()
	require.NoError(t, err1)
	batch1, _ = p1.Value()
	require.NotNil(t, batch1, "batch from first pipeline is nil")

	err2 := p2.Read()
	require.NoError(t, err2)
	batch2, _ = p2.Value()
	require.NotNil(t, batch2, "batch from second pipeline is nil")

	checkSchema(t, batch1, batch2)

	// Compare row by row until both pipelines are exhausted
	for {
		// Both pipelines are exhausted
		if batch1 == nil && batch2 == nil {
			break
		}

		// One pipeline is exhausted but the other isn't - they should be equal
		if (batch1 == nil && batch2 != nil) || (batch1 != nil && batch2 == nil) {
			require.Fail(t, "pipelines have different number of rows")
			break
		}

		// Compare current row
		compareRow(t, batch1, pos1, batch2, pos2, totalRows)
		totalRows++

		// Advance positions
		pos1++
		pos2++

		// If we've consumed all rows in batch1, get the next batch
		if pos1 >= batch1.NumRows() {
			pos1 = 0
			err1 = p1.Read()
			if err1 == nil {
				batch1, _ = p1.Value()
				require.NotNil(t, batch1, "batch from first pipeline is nil")
			} else {
				batch1 = nil // Mark as exhausted
			}
		}

		// If we've consumed all rows in batch2, get the next batch
		if pos2 >= batch2.NumRows() {
			pos2 = 0
			err2 = p2.Read()
			if err2 == nil {
				batch2, _ = p2.Value()
				require.NotNil(t, batch2, "batch from second pipeline is nil")
			} else {
				batch2 = nil // Mark as exhausted
			}
		}
	}
}

// compareRow compares a single row between two batches
func compareRow(t *testing.T, batch1 arrow.Record, pos1 int64, batch2 arrow.Record, pos2 int64, rowNum int64) {
	// Assert that both batches have the same number of columns
	require.Equal(t, batch1.NumCols(), batch2.NumCols(), "row %d: column count differs", rowNum)

	// Iterate over columns and compare values
	for j := 0; j < int(batch1.NumCols()); j++ {
		colName1 := batch1.ColumnName(j)
		col1 := batch1.Column(j)
		col2 := batch2.Column(j)

		// Check if both columns are null
		isNull := col1.IsNull(int(pos1))
		require.Equal(t, isNull, col2.IsNull(int(pos2)), "row %d, column %s: null mismatch", rowNum, colName1)
		if isNull {
			continue
		}

		// check validity
		isValid := col1.IsValid(int(pos1))
		require.Equal(t, isValid, col2.IsValid(int(pos2)), "row %d, column %s: validity mismatch", rowNum, colName1)
		if !isValid {
			continue
		}
		// Compare the values
		require.Equal(t, col1.ValueStr(int(pos1)), col2.ValueStr(int(pos2)), "row %d, column %s: value differs", rowNum, colName1)
	}
}

// checkSchema validates that two records have the same schema
func checkSchema(t *testing.T, a, b arrow.Record) {
	// Check that both records have the same number of columns
	require.Equal(t, a.NumCols(), b.NumCols(), "records have different number of columns")

	// Iterate through columns and check names and data types
	for i := 0; i < int(a.NumCols()); i++ {
		require.Equal(t, a.ColumnName(i), b.ColumnName(i), "column name mismatch at index %d", i)
		require.Equal(t, a.Column(i).DataType(), b.Column(i).DataType(), "column data type mismatch at index %d", i)
	}
}

type fakePipelineGen struct {
	schema *arrow.Schema
	batch  func(offset, sz int64, schema *arrow.Schema) arrow.Record
}

func newFakePipelineGen(schema *arrow.Schema, batch func(offset, sz int64, schema *arrow.Schema) arrow.Record) *fakePipelineGen {
	return &fakePipelineGen{
		schema: schema,
		batch:  batch,
	}
}

func (p *fakePipelineGen) Pipeline(batchSize int64, rows int64) Pipeline {
	var pos int64

	return newGenericPipeline(
		Local,
		func(_ []Pipeline) State {
			if pos >= rows {
				return Exhausted
			}

			batch := p.batch(pos, batchSize, p.schema)
			pos += batch.NumRows()

			return success(batch)
		},
		nil,
	)
}

var (
	autoIncrementingIntPipeline = newFakePipelineGen(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil),
		func(offset, sz int64, schema *arrow.Schema) arrow.Record {
			mem := memory.NewGoAllocator()
			builder := array.NewInt64Builder(mem)
			defer builder.Release()

			for i := int64(0); i < sz; i++ {
				builder.Append(offset + i)
			}

			data := builder.NewArray()
			defer data.Release()

			columns := []arrow.Array{data}
			return array.NewRecord(schema, columns, sz)
		},
	)
)

func TestDataGenEquality(t *testing.T) {
	// Create two different contexts with different batch sizes
	c1 := &Context{
		batchSize: 10, // Small batch size
	}
	c2 := &Context{
		batchSize: 25, // Larger batch size
	}

	// Use the same data generator for both pipelines
	gen := &dataGenerator{
		limit: 100,
	}

	// Create pipelines with different batch sizes
	p1 := c1.executeDataGenerator(context.Background(), gen)
	p2 := c2.executeDataGenerator(context.Background(), gen)

	// Compare the pipelines - should pass even with different batch sizes
	equalPipelines(t, p1, p2)
}

func TestFakePipelineGenEquality(t *testing.T) {
	p1 := autoIncrementingIntPipeline.Pipeline(10, 100)
	p2 := autoIncrementingIntPipeline.Pipeline(25, 100)

	equalPipelines(t, p1, p2)
}
