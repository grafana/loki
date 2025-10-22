package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestNewProjectPipeline(t *testing.T) {
	fields := []arrow.Field{
		semconv.FieldFromFQN("utf8.builtin.name", false),
		semconv.FieldFromFQN("int64.builtin.age", false),
		semconv.FieldFromFQN("utf8.builtin.city", false),
	}

	t.Run("project single column", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrowWithAllocator(alloc, fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (just the "name" column)
		columns := []*physicalpb.ColumnExpression{
			&physicalpb.ColumnExpression{
				Name: "name",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice\nBob\nCharlie"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", true),
		}
		expectedRecord, err := CSVToArrowWithAllocator(alloc, expectedFields, expectedCSV)
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
		columns := []*physicalpb.ColumnExpression{
			&physicalpb.ColumnExpression{
				Name: "name",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
			&physicalpb.ColumnExpression{
				Name: "city",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice,New York\nBob,Boston\nCharlie,Seattle"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", true),
			semconv.FieldFromFQN("utf8.builtin.city", true),
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
		columns := []*physicalpb.ColumnExpression{
			&physicalpb.ColumnExpression{
				Name: "city",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
			&physicalpb.ColumnExpression{
				Name: "age",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
			&physicalpb.ColumnExpression{
				Name: "name",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "New York,30,Alice\nBoston,25,Bob\nSeattle,35,Charlie"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.city", true),
			semconv.FieldFromFQN("int64.builtin.age", true),
			semconv.FieldFromFQN("utf8.builtin.name", true),
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
		columns := []*physicalpb.ColumnExpression{
			&physicalpb.ColumnExpression{
				Name: "name",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
			&physicalpb.ColumnExpression{
				Name: "age",
				Type: physicalpb.COLUMN_TYPE_BUILTIN,
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, columns, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output also split across multiple records
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", true),
			semconv.FieldFromFQN("int64.builtin.age", true),
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
		Type:   physicalpb.COLUMN_TYPE_BUILTIN,
	}
}
