package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func newMultiColumnPipelineGen() *fakePipelineGen {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	}, nil)

	return newFakePipelineGen(schema, func(offset, sz int64, schema *arrow.Schema) arrow.Record {
		mem := memory.NewGoAllocator()
		idBuilder := array.NewInt64Builder(mem)
		defer idBuilder.Release()

		nameBuilder := array.NewStringBuilder(mem)
		defer nameBuilder.Release()

		valueBuilder := array.NewFloat64Builder(mem)
		defer valueBuilder.Release()

		// Generate simple values
		for i := int64(0); i < sz; i++ {
			idBuilder.Append(offset + i)
			nameBuilder.Append("name-" + string(rune(97+offset+i)))
			valueBuilder.Append(float64(offset+i) * 10.0)
		}

		idArray := idBuilder.NewArray()
		defer idArray.Release()

		nameArray := nameBuilder.NewArray()
		defer nameArray.Release()

		valueArray := valueBuilder.NewArray()
		defer valueArray.Release()

		columns := []arrow.Array{idArray, nameArray, valueArray}
		return array.NewRecord(schema, columns, sz)
	})
}

func TestProjection(t *testing.T) {
	// Create a pipeline with multiple columns
	pipelineGen := newMultiColumnPipelineGen()
	totalRows := int64(100)
	batchSize := int64(10)

	tests := []struct {
		name        string
		columnNames []string
	}{
		{
			name:        "project single column",
			columnNames: []string{"id"},
		},
		{
			name:        "project multiple columns",
			columnNames: []string{"id", "value"},
		},
		{
			name:        "project all columns",
			columnNames: []string{"id", "name", "value"},
		},
		{
			name:        "project columns in different order",
			columnNames: []string{"value", "id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fresh pipeline for each test to avoid exhaustion
			inputPipeline := pipelineGen.Pipeline(batchSize, totalRows)

			// Create column expressions for the projection
			columns := make([]physical.ColumnExpression, len(tc.columnNames))
			for i, name := range tc.columnNames {
				columns[i] = &physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: name,
					},
				}
			}

			// Create the projection
			proj := &physical.Projection{
				Columns: columns,
			}

			// Execute the projection
			ctx := &Context{
				batchSize: batchSize,
			}
			projectedPipeline := ctx.executeProjection(context.Background(), proj, []Pipeline{inputPipeline})

			// Read the first batch
			err := projectedPipeline.Read()
			require.NoError(t, err)
			batch, err := projectedPipeline.Value()
			require.NoError(t, err)

			// Check that the record has the correct number of columns
			require.Equal(t, len(tc.columnNames), int(batch.NumCols()))

			// Check that the columns have the expected names
			for i, name := range tc.columnNames {
				require.Equal(t, name, batch.ColumnName(i))
			}

			// Check that all rows are preserved
			rowCount := int64(0)
			for {
				if batch == nil {
					break
				}
				rowCount += batch.NumRows()

				err = projectedPipeline.Read()
				if err != nil {
					break
				}
				batch, _ = projectedPipeline.Value()
			}
			require.Equal(t, totalRows, rowCount)
		})
	}
}

// TestProjectionEquality tests that projection correctly selects only the specified columns
func TestProjectionEquality(t *testing.T) {
	// Create test context
	ctx := &Context{
		batchSize: 10,
	}

	// Create a pipeline with all columns (id, name, value)
	fullPipelineGen := newMultiColumnPipelineGen()

	// Create a pipeline that already has only the columns we want to project to
	projectedSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	}, nil)

	expectedPipelineGen := newFakePipelineGen(projectedSchema, func(offset, sz int64, schema *arrow.Schema) arrow.Record {
		mem := memory.NewGoAllocator()
		idBuilder := array.NewInt64Builder(mem)
		defer idBuilder.Release()

		valueBuilder := array.NewFloat64Builder(mem)
		defer valueBuilder.Release()

		// Generate values matching those in the full pipeline
		for i := int64(0); i < sz; i++ {
			idBuilder.Append(offset + i)
			valueBuilder.Append(float64(offset+i) * 10.0)
		}

		idArray := idBuilder.NewArray()
		defer idArray.Release()

		valueArray := valueBuilder.NewArray()
		defer valueArray.Release()

		columns := []arrow.Array{idArray, valueArray}
		return array.NewRecord(schema, columns, sz)
	})

	// Define projection columns - only id and value
	columns := []physical.ColumnExpression{
		&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "id",
			},
		},
		&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "value",
			},
		},
	}

	// Create the projection specification
	proj := &physical.Projection{
		Columns: columns,
	}

	// Total rows and batch size for both pipelines
	totalRows := int64(100)

	// Create input pipeline with all columns
	inputPipeline := fullPipelineGen.Pipeline(10, totalRows)

	// Create the projected pipeline by applying projection to the input
	projectedPipeline := ctx.executeProjection(context.Background(), proj, []Pipeline{inputPipeline})

	// Create expected pipeline that already has only the projected columns
	expectedPipeline := expectedPipelineGen.Pipeline(10, totalRows)

	// Compare the projected pipeline with the expected pipeline
	// The results should be identical
	equalPipelines(t, projectedPipeline, expectedPipeline)
}
