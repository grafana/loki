package executor

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
)

// AssertPipelinesEqual iterates through two pipelines and ensures they contain
// the same data, regardless of batch sizes. It compares row by row and column by column.
func AssertPipelinesEqual(t testing.TB, left, right Pipeline) {
	defer left.Close()
	defer right.Close()

	var (
		leftBatch, rightBatch   arrow.Record
		leftPos, rightPos       int64
		leftRemain, rightRemain int64
		rowNum                  int64
		leftErr, rightErr       error
	)

	for {
		// Load batches if needed
		if leftRemain == 0 {
			if leftBatch != nil {
				leftBatch.Release()
				leftBatch = nil
			}

			leftErr = left.Read()
			if leftErr == nil {
				leftBatch, leftErr = left.Value()
				if leftErr == nil {
					leftRemain = leftBatch.NumRows()
					leftPos = 0
				}
			}
		}

		if rightRemain == 0 {
			if rightBatch != nil {
				rightBatch.Release()
				rightBatch = nil
			}

			rightErr = right.Read()
			if rightErr == nil {
				rightBatch, rightErr = right.Value()
				if rightErr == nil {
					rightRemain = rightBatch.NumRows()
					rightPos = 0
				}
			}
		}

		// Check conditions on our batches:
		switch {
		case errors.Is(leftErr, EOF) && errors.Is(rightErr, EOF):
			// Both pipelines are finished; we can finish now.
			return

		case (errors.Is(leftErr, EOF) && rightErr == nil) || (errors.Is(rightErr, EOF) && leftErr == nil):
			// One pipeline finished before the other (and the other didn't fail), then
			// there's an inequal number of rows.
			require.Fail(t, "Pipelines have a different number of rows",
				"left error: %v, right error: %v", leftErr, rightErr)

		case leftErr != nil && rightErr != nil && !errors.Is(leftErr, EOF) && !errors.Is(rightErr, EOF):
			// Both pipelines failed with non-EOF errors.
			require.Equal(t, leftErr, rightErr, "Pipelines failed with different errors")

		case leftErr != nil && !errors.Is(leftErr, EOF):
			// Left pipeline failed with a non-EOF error.
			require.Fail(t, "Left pipeline failed", "error: %v", leftErr)

		case rightErr != nil && !errors.Is(rightErr, EOF):
			// Right pipeline failed with a non-EOF error.
			require.Fail(t, "Right pipeline failed", "error: %v", rightErr)
		}

		// Check schema compatibility
		require.True(t, leftBatch.Schema().Equal(rightBatch.Schema()),
			"Pipelines have incompatible schemas: %v vs %v",
			leftBatch.Schema(), rightBatch.Schema())

		// Compare rows until one of the batches is exhausted
		compareRows := min(leftRemain, rightRemain)
		for i := int64(0); i < compareRows; i++ {
			// Compare all columns for this row
			for j := 0; j < int(leftBatch.NumCols()); j++ {
				colName := leftBatch.ColumnName(j)
				leftCol := leftBatch.Column(j)
				rightCol := rightBatch.Column(j)

				// Check if both columns are null
				leftIsNull := leftCol.IsNull(int(leftPos))
				rightIsNull := rightCol.IsNull(int(rightPos))
				require.Equal(t, leftIsNull, rightIsNull,
					"row %d, column %s: null mismatch", rowNum, colName)

				if leftIsNull {
					continue
				}

				// Check validity
				leftIsValid := leftCol.IsValid(int(leftPos))
				rightIsValid := rightCol.IsValid(int(rightPos))
				require.Equal(t, leftIsValid, rightIsValid,
					"row %d, column %s: validity mismatch", rowNum, colName)

				if !leftIsValid {
					continue
				}

				// Compare the values
				require.Equal(t, leftCol.ValueStr(int(leftPos)), rightCol.ValueStr(int(rightPos)),
					"row %d, column %s: value differs", rowNum, colName)
			}

			leftPos++
			rightPos++
			leftRemain--
			rightRemain--
			rowNum++
		}
	}
}

func TestAssertPipelinesEqual(t *testing.T) {
	// Define test schema
	fields := []arrow.Field{
		{Name: "name", Type: datatype.Arrow.String},
		{Name: "age", Type: datatype.Arrow.Integer},
	}

	// Create test data
	csvData := "Alice,30\nBob,25\nCharlie,35\nDave,40"
	record, err := CSVToArrow(fields, csvData)
	require.NoError(t, err)
	defer record.Release()

	// Split the record into two smaller records
	csvData1 := "Alice,30\nBob,25"
	csvData2 := "Charlie,35\nDave,40"
	record1, err := CSVToArrow(fields, csvData1)
	require.NoError(t, err)
	defer record1.Release()
	record2, err := CSVToArrow(fields, csvData2)
	require.NoError(t, err)
	defer record2.Release()

	t.Run("identical pipelines with same batch size", func(t *testing.T) {
		// Retain records to use in multiple pipelines
		record.Retain()
		record.Retain()
		p1 := NewBufferedPipeline(record)
		p2 := NewBufferedPipeline(record)

		// This should not fail
		AssertPipelinesEqual(t, p1, p2)
	})

	t.Run("identical pipelines with different batch sizes", func(t *testing.T) {
		// First pipeline has one batch with all data
		record.Retain()
		p1 := NewBufferedPipeline(record)

		// Second pipeline has two smaller batches
		record1.Retain()
		record2.Retain()
		p2 := NewBufferedPipeline(record1, record2)

		// This should not fail despite different batch sizes
		AssertPipelinesEqual(t, p1, p2)
	})
}
