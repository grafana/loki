package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// newNumberPipelineGen creates a pipeline with id and value columns
// with incremental values
func newNumberPipelineGen() *fakePipelineGen {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	}, nil)

	return newFakePipelineGen(schema, func(offset, sz int64, schema *arrow.Schema) arrow.Record {
		mem := memory.NewGoAllocator()
		idBuilder := array.NewInt64Builder(mem)
		defer idBuilder.Release()

		valueBuilder := array.NewFloat64Builder(mem)
		defer valueBuilder.Release()

		// Generate simple values
		for i := int64(0); i < sz; i++ {
			idBuilder.Append(offset + i)
			valueBuilder.Append(float64(offset + i))
		}

		idArray := idBuilder.NewArray()
		defer idArray.Release()

		valueArray := valueBuilder.NewArray()
		defer valueArray.Release()

		columns := []arrow.Array{idArray, valueArray}
		return array.NewRecord(schema, columns, sz)
	})
}

func TestFilter(t *testing.T) {
	// Simple test to verify filter passthrough
	ctx := &Context{
		batchSize: 10,
	}

	// Generate pipeline with 100 rows
	totalRows := int64(100)
	pipelineGen := newNumberPipelineGen()
	inputPipeline := pipelineGen.Pipeline(ctx.batchSize, totalRows)

	// Empty filter should pass through all rows
	emptyFilter := &physical.Filter{
		Predicates: []physical.Expression{},
	}

	filteredPipeline := ctx.executeFilter(context.Background(), emptyFilter, []Pipeline{inputPipeline})

	// Count rows in result
	var rowCount int64
	for {
		err := filteredPipeline.Read()
		if err != nil {
			break
		}

		batch, err := filteredPipeline.Value()
		require.NoError(t, err)
		rowCount += batch.NumRows()
	}

	// Should have the same number of rows as input
	require.Equal(t, totalRows, rowCount)
}
