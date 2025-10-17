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
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice\nBob\nCharlie"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
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
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("city"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice,New York\nBob,Boston\nCharlie,Seattle"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
			semconv.FieldFromFQN("utf8.builtin.city", false),
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
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("age"),
			},
		}

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &expressionEvaluator{})
		require.NoError(t, err)

		// Create expected output also split across multiple records
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
			semconv.FieldFromFQN("int64.builtin.age", false),
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

	t.Run("drop", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.service", false),
				semconv.FieldFromFQN("int64.metadata.count", false),
				semconv.FieldFromFQN("int64.parsed.count", false),
			}, nil)

		rows := arrowtest.Rows{
			{"utf8.builtin.service": "loki", "int64.metadata.count": 1, "int64.parsed.count": 1},
			{"utf8.builtin.service": "loki", "int64.metadata.count": 2, "int64.parsed.count": 4},
			{"utf8.builtin.service": "loki", "int64.metadata.count": 3, "int64.parsed.count": 9},
		}

		for _, tc := range []struct {
			name           string
			columns        []physical.Expression
			expectedFields []arrow.Field
		}{
			{
				name: "single column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("int64.metadata.count", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
			{
				name: "single ambiguous column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
				},
			},
			{
				name: "single non-ambiguous column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeMetadata}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
			{
				name: "multiple columns",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeBuiltin}},
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeParsed}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("int64.metadata.count", false),
				},
			},
			{
				name: "non existent columns",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "__error__", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
					semconv.FieldFromFQN("int64.metadata.count", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				// Create input pipeline
				alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer alloc.AssertSize(t, 0) // Assert empty on test exit

				// Create input data with message column containing logfmt
				input := NewArrowtestPipeline(alloc, schema, rows)

				// Create project pipeline
				proj := &physical.Projection{
					Expressions: tc.columns,
					All:         true,
					Drop:        true,
				}
				pipeline, err := NewProjectPipeline(input, proj, &expressionEvaluator{})
				require.NoError(t, err)

				ctx := t.Context()
				record, err := pipeline.Read(ctx)
				require.NoError(t, err)
				defer record.Release()

				// Verify the output has the expected number of fields
				outputSchema := record.Schema()
				require.Equal(t, len(tc.expectedFields), outputSchema.NumFields())
				require.Equal(t, tc.expectedFields, outputSchema.Fields())
			})
		}

	})

}

// Helper to create a column reference
func createColumnRef(name string) types.ColumnRef {
	return types.ColumnRef{
		Column: name,
		Type:   types.ColumnTypeBuiltin,
	}
}
