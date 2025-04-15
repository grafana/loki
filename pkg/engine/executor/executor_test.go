package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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

func collect(t *testing.T, pipeline Pipeline) (batches int64, rows int64) {
	for {
		err := pipeline.Read()
		if err == EOF {
			break
		}
		if err != nil {
			t.Fatalf("did not expect error, got %s", err.Error())
		}
		batch, _ := pipeline.Value()
		t.Log("batch", batch, "err", err)
		batches++
		rows += batch.NumRows()
	}
	return batches, rows
}
