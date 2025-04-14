package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func equalPipelines(t *testing.T, p1, p2 Pipeline) {
	for {
		leftErr := p1.Read()
		rightErr := p2.Read()

		left, _ := p1.Value()
		right, _ := p2.Value()

		if leftErr != nil {
			require.Equal(t, leftErr, rightErr, "error mismatch")
			break
		}

		equalBatch(t, left, right)
	}
}

func equalBatch(t *testing.T, b1, b2 arrow.Record) {
	// Check for nil records
	require.NotNil(t, b1, "first record is nil")
	require.NotNil(t, b2, "second record is nil")

	// rows
	require.Equal(t, b1.NumRows(), b2.NumRows(), "number of rows mismatch")
	// columns
	require.Equal(t, b1.NumCols(), b2.NumCols(), "number of columns mismatch")

	for i := range b1.Columns() {
		// column names
		require.Equal(t, b1.ColumnName(i), b2.ColumnName(i), "column name mismatch")
		// column types
		require.Equal(t, b1.Column(i).DataType(), b2.Column(i).DataType(), "column type mismatch")

		// column values
		left, right := b1.Column(i), b2.Column(i)
		for j := range left.Len() {
			// null check
			require.Equal(t, left.IsNull(j), right.IsNull(j), "null value mismatch")
			// isValid check
			require.Equal(t, left.IsValid(j), right.IsValid(j), "value mismatch")
			// value check
			require.Equal(t, left.ValueStr(j), right.ValueStr(j), "value mismatch")
		}
	}
}

func TestDataGenEquality(t *testing.T) {
	c := &Context{
		batchSize: 10,
	}
	gen := &dataGenerator{
		limit: 100,
	}

	p1 := c.executeDataGenerator(context.Background(), gen)
	p2 := c.executeDataGenerator(context.Background(), gen)

	equalPipelines(t, p1, p2)
}
