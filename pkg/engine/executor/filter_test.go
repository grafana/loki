package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestNewFilterPipeline(t *testing.T) {
	fields := []arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		{Name: "valid", Type: arrow.FixedWidthTypes.Boolean, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Bool)},
	}

	t.Run("filter with true literal predicate", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,true\nBob,false\nCharlie,true"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create a filter predicate that's always true
		truePredicate := physical.NewLiteral(true)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{truePredicate},
		}

		// Create filter pipeline
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (should be same as input since predicate is always true)
		expectedRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})

	t.Run("filter with false literal predicate", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,true\nBob,false\nCharlie,true"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create a filter predicate that's always false
		falsePredicate := physical.NewLiteral(false)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{falsePredicate},
		}

		// Create filter pipeline
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (should be empty since predicate is always false)
		schema := arrow.NewSchema(fields, nil)
		mem := memory.NewGoAllocator()

		// Create empty arrays for each field
		emptyArrays := make([]arrow.Array, len(fields))
		emptyArrays[0] = array.NewStringBuilder(mem).NewArray()  // empty string array
		emptyArrays[1] = array.NewBooleanBuilder(mem).NewArray() // empty boolean array

		expectedRecord := array.NewRecord(schema, emptyArrays, 0)
		defer expectedRecord.Release()
		// Be sure to release the arrays too
		defer func() {
			for _, arr := range emptyArrays {
				arr.Release()
			}
		}()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})

	t.Run("filter on boolean column with column expression", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,true\nBob,false\nCharlie,true"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create a filter predicate that uses the 'valid' column directly
		validColumnPredicate := &physical.ColumnExpr{
			Ref: createColumnRef("valid"),
		}

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{validColumnPredicate},
		}

		// Create filter pipeline
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (only rows where valid=true)
		expectedCSV := "Alice,true\nCharlie,true"
		expectedRecord, err := CSVToArrow(fields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})

	t.Run("filter on multiple columns with binary expressions", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,true\nBob,false\nBob,true\nCharlie,false"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

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
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (only rows where name=="Bob" AND valid!=false)
		expectedCSV := "Bob,true"
		expectedRecord, err := CSVToArrow(fields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})

	t.Run("filter on empty batch", func(t *testing.T) {
		// Create empty record with correct schema
		schema := arrow.NewSchema(fields, nil)
		mem := memory.NewGoAllocator()

		// Create empty arrays for each field
		emptyArrays := make([]arrow.Array, len(fields))
		emptyArrays[0] = array.NewStringBuilder(mem).NewArray()  // empty string array
		emptyArrays[1] = array.NewBooleanBuilder(mem).NewArray() // empty boolean array

		emptyRecord := array.NewRecord(schema, emptyArrays, 0)
		defer emptyRecord.Release()
		// Be sure to release the arrays too
		defer func() {
			for _, arr := range emptyArrays {
				arr.Release()
			}
		}()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(emptyRecord)

		// Create a simple filter
		truePredicate := physical.NewLiteral(true)

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{truePredicate},
		}

		// Create filter pipeline
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (also empty)
		mem2 := memory.NewGoAllocator()

		// Create empty arrays for each field
		expectedArrays := make([]arrow.Array, len(fields))
		expectedArrays[0] = array.NewStringBuilder(mem2).NewArray()  // empty string array
		expectedArrays[1] = array.NewBooleanBuilder(mem2).NewArray() // empty boolean array

		expectedRecord := array.NewRecord(schema, expectedArrays, 0)
		defer expectedRecord.Release()
		// Be sure to release the arrays too
		defer func() {
			for _, arr := range expectedArrays {
				arr.Release()
			}
		}()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})

	t.Run("filter with multiple input batches", func(t *testing.T) {
		// Create input data split across multiple records
		inputCSV1 := "Alice,true\nBob,false"
		inputCSV2 := "Charlie,true\nDave,false"

		inputRecord1, err := CSVToArrow(fields, inputCSV1)
		require.NoError(t, err)
		defer inputRecord1.Release()

		inputRecord2, err := CSVToArrow(fields, inputCSV2)
		require.NoError(t, err)
		defer inputRecord2.Release()

		// Create input pipeline with multiple batches
		inputPipeline := NewBufferedPipeline(inputRecord1, inputRecord2)

		// Create a filter predicate that uses the 'valid' column directly
		validColumnPredicate := &physical.ColumnExpr{
			Ref: createColumnRef("valid"),
		}

		// Create a Filter node
		filter := &physical.Filter{
			Predicates: []physical.Expression{validColumnPredicate},
		}

		// Create filter pipeline
		filterPipeline := NewFilterPipeline(filter, inputPipeline, expressionEvaluator{})

		// Create expected output (only rows where valid=true)
		expectedCSV := `
Alice,true
Charlie,true
		`
		expectedRecord, err := CSVToArrow(fields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, filterPipeline, expectedPipeline)
	})
}
