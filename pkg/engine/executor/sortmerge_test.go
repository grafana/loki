package executor

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestSortMerge(t *testing.T) {
	now := time.Unix(1000000, 0)
	var batchSize = int64(3)

	c := &Context{
		batchSize: batchSize,
	}

	t.Run("invalid column name", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "not_a_timestamp_column",
					Type:   types.ColumnTypeBuiltin,
				},
			},
		}

		inputs := []Pipeline{
			ascendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 10),
			ascendingTimestampPipeline(now.Add(2*time.Nanosecond)).Pipeline(batchSize, 10),
			ascendingTimestampPipeline(now.Add(3*time.Nanosecond)).Pipeline(batchSize, 10),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		ctx := t.Context()
		err = pipeline.Read(ctx)
		require.ErrorContains(t, err, "column is not a timestamp column")
	})

	t.Run("ascending timestamp", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: types.ColumnNameBuiltinTimestamp,
					Type:   types.ColumnTypeBuiltin,
				},
			},
			Order: physical.ASC,
		}

		inputs := []Pipeline{
			ascendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 10),
			ascendingTimestampPipeline(now.Add(2*time.Millisecond)).Pipeline(batchSize, 10),
			ascendingTimestampPipeline(now.Add(3*time.Second)).Pipeline(batchSize, 10),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		ctx := t.Context()
		timestamps := make([]arrow.Timestamp, 0, 30)
		var batches, rows int64
		for {
			err := pipeline.Read(ctx)
			if err == EOF {
				break
			}
			if err != nil {
				t.Fatalf("did not expect error, got %s", err.Error())
			}
			batch, _ := pipeline.Value()

			tsCol, err := c.evaluator.eval(merge.Column, batch)
			require.NoError(t, err)
			arr := tsCol.ToArray().(*array.Timestamp)

			timestamps = append(timestamps, arr.Values()...)
			batches++
			rows += batch.NumRows()
		}

		// Check if ts column is sorted
		require.Truef(t,
			slices.IsSortedFunc(timestamps, func(a, b arrow.Timestamp) int { return int(a - b) }),
			"timestamps are not sorted in ASC order: %v", timestamps)
	})

	t.Run("descending timestamp", func(t *testing.T) {
		merge := &physical.SortMerge{
			Column: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: types.ColumnNameBuiltinTimestamp,
					Type:   types.ColumnTypeBuiltin,
				},
			},
			Order: physical.DESC,
		}

		inputs := []Pipeline{
			descendingTimestampPipeline(now.Add(1*time.Nanosecond)).Pipeline(batchSize, 10),
			descendingTimestampPipeline(now.Add(2*time.Millisecond)).Pipeline(batchSize, 10),
			descendingTimestampPipeline(now.Add(3*time.Second)).Pipeline(batchSize, 10),
		}

		pipeline, err := NewSortMergePipeline(inputs, merge.Order, merge.Column, expressionEvaluator{})
		require.NoError(t, err)

		ctx := t.Context()
		timestamps := make([]arrow.Timestamp, 0, 30)
		var batches, rows int64
		for {
			err := pipeline.Read(ctx)
			if err == EOF {
				break
			}
			if err != nil {
				t.Fatalf("did not expect error, got %s", err.Error())
			}
			batch, _ := pipeline.Value()

			tsCol, err := c.evaluator.eval(merge.Column, batch)
			require.NoError(t, err)
			arr := tsCol.ToArray().(*array.Timestamp)

			timestamps = append(timestamps, arr.Values()...)
			batches++
			rows += batch.NumRows()
		}

		// Check if ts column is sorted
		require.Truef(t,
			slices.IsSortedFunc(timestamps, func(a, b arrow.Timestamp) int { return int(b - a) }),
			"timestamps are not sorted in DESC order: %v", timestamps)
	})
}
