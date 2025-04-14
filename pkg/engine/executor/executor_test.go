package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestDataGenerator(t *testing.T) {

	for _, tt := range []struct {
		name            string
		limit           int64
		batchSize       int64
		expectedBatches int64
		expectedRows    int64
	}{
		{
			name:            "limit is multiple of batch size",
			limit:           10,
			batchSize:       2,
			expectedBatches: 5,
			expectedRows:    10,
		},
		{
			name:            "limit is not multiple of batch size",
			limit:           10,
			batchSize:       7,
			expectedBatches: 2,
			expectedRows:    10,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := physical.NewBuilder()
			b = b.Add(&dataGenerator{
				limit: tt.limit,
			})

			pipeline := Run(context.Background(), Config{BatchSize: tt.batchSize}, b.Plan())
			batches, rows := collect(t, pipeline)

			require.Equal(t, tt.expectedBatches, batches)
			require.Equal(t, tt.expectedRows, rows)
		})
	}
}

func TestLimit(t *testing.T) {
	for _, tt := range []struct {
		name            string
		offset          uint32
		limit           uint32
		batchSize       int64
		expectedBatches int64
		expectedRows    int64
	}{
		{
			name:            "without offset",
			offset:          0,
			limit:           5,
			batchSize:       3,
			expectedBatches: 2,
			expectedRows:    5,
		},
		{
			name:            "with offset",
			offset:          3,
			limit:           5,
			batchSize:       4,
			expectedBatches: 2,
			expectedRows:    5,
		},
		{
			name:            "with offset greater than batch size",
			offset:          5,
			limit:           6,
			batchSize:       2,
			expectedBatches: 6, // TODO: skip empty batches
			expectedRows:    6,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := physical.NewBuilder()
			b = b.Add(&physical.Limit{
				Skip:  tt.offset,
				Fetch: tt.limit,
			})
			_ = b.Add(&dataGenerator{
				limit: 100,
			})

			pipeline := Run(context.Background(), Config{BatchSize: tt.batchSize}, b.Plan())
			batches, rows := collect(t, pipeline)

			require.Equal(t, tt.expectedBatches, batches)
			require.Equal(t, tt.expectedRows, rows)
		})
	}
}

func TestSortMerge(t *testing.T) {
	b := physical.NewBuilder()
	b = b.Add(&physical.SortMerge{
		Column: &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "timestamp",
				Type:   types.ColumnTypeBuiltin,
			},
		},
	})
	_ = b.Add(&dataGenerator{
		limit: 10,
	})
	_ = b.Add(&dataGenerator{
		limit: 10,
	})
	_ = b.Add(&dataGenerator{
		limit: 10,
	})

	pipeline := Run(context.Background(), Config{BatchSize: 10}, b.Plan())
	batches, rows := collect(t, pipeline)

	require.Equal(t, int64(3), batches)
	require.Equal(t, int64(30), rows)
}

func collect(t *testing.T, pipeline Pipeline) (batches int64, rows int64) {
	for {
		err := pipeline.Read()
		if err == EOF {
			break
		}
		if err != nil {
			t.Fatalf("did not expect error, got %s", err.Error())
			break
		}
		batch, _ := pipeline.Value()
		t.Log("batch", batch, "err", err)
		batches++
		rows += batch.NumRows()
	}
	return batches, rows
}
