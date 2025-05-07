package executor

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestSortMerge(t *testing.T) {
	now := time.Date(2024, 04, 15, 0, 0, 0, 0, time.UTC)
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

		inputs := []Pipeline{
			ascendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 100),
			ascendingTimestampPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 100),
			ascendingTimestampPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 100),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		err = pipeline.Read()
		require.ErrorContains(t, err, "key error")
	})

	t.Run("ascending timestamp", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "timestamp",
					Type:   types.ColumnTypeBuiltin,
				},
			},
			Order: physical.ASC,
		}

		inputs := []Pipeline{
			ascendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 100),
			ascendingTimestampPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 100),
			ascendingTimestampPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 100),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		var lastTs int64
		var batches, rows int64
		for {
			err := pipeline.Read()
			if err == EOF {
				break
			}
			if err != nil {
				t.Fatalf("did not expect error, got %s", err.Error())
			}
			batch, _ := pipeline.Value()

			tsCol, err := c.evaluator.eval(merge.Column, batch)
			require.NoError(t, err)
			arr := tsCol.ToArray().(*array.Int64)

			// Check if ts column is sorted
			for i := 0; i < arr.Len()-1; i++ {
				require.LessOrEqual(t, arr.Value(i), arr.Value(i+1))
				// also check ascending order across batches
				require.GreaterOrEqual(t, arr.Value(i), lastTs)
				lastTs = arr.Value(i + 1)
			}
			batches++
			rows += batch.NumRows()
		}

		// The test scenario is worst case and produces single-row records.
		// require.Equal(t, int64(30), batches)
		require.Equal(t, int64(300), rows)
	})

	t.Run("descending timestamp", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "timestamp",
					Type:   types.ColumnTypeBuiltin,
				},
			},
			Order: physical.DESC,
		}

		inputs := []Pipeline{
			descendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 100),
			descendingTimestampPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 100),
			descendingTimestampPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 100),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		var lastTs int64 = math.MaxInt64
		var batches, rows int64
		for {
			err := pipeline.Read()
			if err == EOF {
				break
			}
			if err != nil {
				t.Fatalf("did not expect error, got %s", err.Error())
			}
			batch, _ := pipeline.Value()

			tsCol, err := c.evaluator.eval(merge.Column, batch)
			require.NoError(t, err)
			arr := tsCol.ToArray().(*array.Int64)

			// Check if ts column is sorted
			for i := 0; i < arr.Len()-1; i++ {
				require.GreaterOrEqual(t, arr.Value(i), arr.Value(i+1))
				// also check descending order across batches
				require.LessOrEqual(t, arr.Value(i), lastTs)
				lastTs = arr.Value(i + 1)
			}
			batches++
			rows += batch.NumRows()
		}

		// The test scenario is worst case and produces single-row records.
		// require.Equal(t, int64(30), batches)
		require.Equal(t, int64(300), rows)
	})
}
