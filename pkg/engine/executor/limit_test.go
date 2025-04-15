package executor

import (
	"context"
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/stretchr/testify/require"
)

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
			c := &Context{
				batchSize: tt.batchSize,
			}
			limit := &physical.Limit{
				Skip:  tt.offset,
				Fetch: tt.limit,
			}
			inputs := []Pipeline{
				autoIncrementingIntPipeline.Pipeline(tt.batchSize, 1000),
			}

			pipeline := c.executeLimit(context.Background(), limit, inputs)
			batches, rows := collect(t, pipeline)

			require.Equal(t, tt.expectedBatches, batches)
			require.Equal(t, tt.expectedRows, rows)
		})
	}
}
