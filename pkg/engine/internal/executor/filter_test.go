package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewFilterPipeline(t *testing.T) {
	colName := "utf8.builtin.name"
	colValid := "bool.builtin.valid"

	fields := []arrow.Field{
		semconv.FieldFromFQN(colName, true),
		semconv.FieldFromFQN(colValid, true),
	}

	t.Run("filter with true literal predicate", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: "Bob", colValid: false},
				{colName: "Charlie", colValid: true},
			},
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)

		// Create a filter predicate that's always true
		truePredicate := physical.NewLiteral(true)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{truePredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(inputRows[0]), len(rows), "number of rows should match")
		require.ElementsMatch(t, inputRows[0], rows)
	})

	t.Run("filter with false literal predicate", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: "Bob", colValid: false},
				{colName: "Charlie", colValid: true},
			},
		}

		// Create input pipeline
		input := NewArrowtestPipeline(alloc, schema, inputRows...)

		// Create a filter predicate that's always false
		falsePredicate := physical.NewLiteral(false)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{falsePredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		require.Equal(t, int64(0), record.NumRows(), "should not return any rows")
	})

	t.Run("filter on boolean column with column expression", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: "Bob", colValid: false},
				{colName: "Charlie", colValid: true},
			},
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)
		defer input.Close()

		// Create a filter predicate that uses the 'valid' column directly
		validColumnPredicate := &physical.ColumnExpr{
			Ref: createColumnRef("valid"),
		}

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{validColumnPredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Create expected output (only rows where valid=true)
		expectedRows := arrowtest.Rows{
			{colName: "Alice", colValid: true},
			{colName: "Charlie", colValid: true},
		}

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expectedRows), len(rows), "number of rows should match")
		require.ElementsMatch(t, expectedRows, rows)
	})

	t.Run("filter on multiple columns with binary expressions", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: "Bob", colValid: false},
				{colName: "Bob", colValid: true},
				{colName: "Charlie", colValid: false},
			},
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)
		defer input.Close()

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{
				&physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: createColumnRef("name")},
					Right: physical.NewLiteral("Bob"),
					Op:    types.BinaryOpEq,
				},
				&physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: createColumnRef("valid")},
					Right: physical.NewLiteral(false),
					Op:    types.BinaryOpNeq,
				},
			},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Create expected output (only rows where name=="Bob" AND valid!=false)
		expectedRows := arrowtest.Rows{
			{colName: "Bob", colValid: true},
		}

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expectedRows), len(rows), "number of rows should match")
		require.ElementsMatch(t, expectedRows, rows)
	})

	// TODO: instead of returning empty batch, filter should read the next non-empty batch.
	t.Run("filter on empty batch", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create empty input data using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{}, // empty rows
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)
		defer input.Close()

		// Create a simple filter
		truePredicate := physical.NewLiteral(true)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{truePredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, 0, len(rows), "should return empty batch")
	})

	t.Run("filter with multiple input batches", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data split across multiple batches using arrowtest.Rows
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: "Bob", colValid: false},
			},
			{
				{colName: "Charlie", colValid: true},
				{colName: "Dave", colValid: false},
			},
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)
		defer input.Close()

		// Create a filter predicate that uses the 'valid' column directly
		validColumnPredicate := &physical.ColumnExpr{
			Ref: createColumnRef("valid"),
		}

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{validColumnPredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Create expected output (only rows where valid=true)
		expectedRows := arrowtest.Rows{
			{colName: "Alice", colValid: true},
			{colName: "Charlie", colValid: true},
		}

		// Read the pipeline output
		record1, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record1.Release()

		rows, err := arrowtest.RecordRows(record1)
		require.NoError(t, err, "should be able to convert record back to rows")

		got := rows

		// read second batch
		record2, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record2.Release()

		rows, err = arrowtest.RecordRows(record2)
		require.NoError(t, err, "should be able to convert record back to rows")
		got = append(got, rows...)

		require.Equal(t, len(expectedRows), len(got), "number of rows should match")
		require.ElementsMatch(t, expectedRows, got)
	})

	t.Run("filter with null values", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema(fields, nil)

		// Create input data with null values
		inputRows := []arrowtest.Rows{
			{
				{colName: "Alice", colValid: true},
				{colName: nil, colValid: true}, // null name
				{colName: "Bob", colValid: false},
			},
		}
		input := NewArrowtestPipeline(alloc, schema, inputRows...)
		defer input.Close()

		// Create a filter predicate that uses the 'valid' column directly
		validColumnPredicate := &physical.ColumnExpr{
			Ref: createColumnRef("valid"),
		}

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{validColumnPredicate},
		}

		// Create filter pipeline
		e := newExpressionEvaluator(alloc)
		pipeline := NewFilterPipeline(filter, input, e, alloc)
		defer pipeline.Close()

		// Create expected output (only rows where valid=true, including null name)
		expectedRows := arrowtest.Rows{
			{colName: "Alice", colValid: true},
			{colName: nil, colValid: true}, // null name should be retained
		}

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expectedRows), len(rows), "number of rows should match")
		require.ElementsMatch(t, expectedRows, rows)
	})
}
