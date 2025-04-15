package executor

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func timestampIncrementingPipeline(start time.Time) *fakePipelineGen {
	return newFakePipelineGen(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64},
		}, nil),

		func(offset, sz int64, schema *arrow.Schema) arrow.Record {
			mem := memory.NewGoAllocator()
			idColBuilder := array.NewInt64Builder(mem)
			defer idColBuilder.Release()

			tsColBuilder := array.NewUint64Builder(mem)
			defer tsColBuilder.Release()

			for i := int64(0); i < sz; i++ {
				idColBuilder.Append(offset + i)
				tsColBuilder.Append(uint64(start.Add(time.Duration(offset)*time.Second + time.Duration(i)*time.Millisecond).UnixNano()))
			}

			idData := idColBuilder.NewArray()
			defer idData.Release()

			tsData := tsColBuilder.NewArray()
			defer tsData.Release()

			columns := []arrow.Array{idData, tsData}
			return array.NewRecord(schema, columns, sz)
		},
	)
}

func TestSortMerge(t *testing.T) {
	var batchSize = int64(10)

	c := &Context{
		batchSize: batchSize,
	}

	t.Run("invalid column name", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "invalid",
					Type:   types.ColumnTypeBuiltin,
				},
			},
		}

		now := time.Date(2024, 04, 15, 0, 0, 0, 0, time.UTC)
		inputs := []Pipeline{
			timestampIncrementingPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 100),
			timestampIncrementingPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 100),
			timestampIncrementingPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 100),
		}

		pipeline := c.executeSortMerge(context.Background(), merge, inputs)

		err := pipeline.Read()
		require.ErrorContains(t, err, "key error")
	})

	t.Run("valid column name", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "timestamp",
					Type:   types.ColumnTypeBuiltin,
				},
			},
		}

		now := time.Date(2024, 04, 15, 0, 0, 0, 0, time.UTC)
		inputs := []Pipeline{
			timestampIncrementingPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 100),
			timestampIncrementingPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 100),
			timestampIncrementingPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 100),
		}

		pipeline := c.executeSortMerge(context.Background(), merge, inputs)

		batches, rows := collect(t, pipeline)
		require.Equal(t, int64(30), batches)
		require.Equal(t, int64(300), rows)
	})
}
