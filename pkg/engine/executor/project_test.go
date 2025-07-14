package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestNewProjectPipeline(t *testing.T) {
	fields := []arrow.Field{
		{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
		{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
	}

	t.Run("project single column", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (just the "name" column)
		columns := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice\nBob\nCharlie"
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project multiple columns", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (both "name" and "city" columns)
		columns := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("city"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice,New York\nBob,Boston\nCharlie,Seattle"
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project columns in different order", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (reordering columns)
		columns := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: createColumnRef("city"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("age"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "New York,30,Alice\nBoston,25,Bob\nSeattle,35,Charlie"
		expectedFields := []arrow.Field{
			{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project with multiple input batches", func(t *testing.T) {
		// Create input data split across multiple records
		inputCSV1 := "Alice,30,New York\nBob,25,Boston"
		inputCSV2 := "Charlie,35,Seattle\nDave,40,Portland"

		inputRecord1, err := CSVToArrow(fields, inputCSV1)
		require.NoError(t, err)
		defer inputRecord1.Release()

		inputRecord2, err := CSVToArrow(fields, inputCSV2)
		require.NoError(t, err)
		defer inputRecord2.Release()

		// Create input pipeline with multiple batches
		inputPipeline := NewBufferedPipeline(inputRecord1, inputRecord2)

		// Create projection columns
		columns := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("age"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output also split across multiple records
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
		}

		expected := `
Alice,30
Bob,25
Charlie,35
Dave,40
		`

		expectedRecord, err := CSVToArrow(expectedFields, expected)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

}

// Helper to create a column reference
func createColumnRef(name string) types.ColumnRef {
	return types.ColumnRef{
		Column: name,
		Type:   types.ColumnTypeBuiltin,
	}
}
