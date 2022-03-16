package aws

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v2/pkg/storage/chunk"
	"github.com/grafana/loki/v2/pkg/storage/chunk/testutils"
)

const (
	tableName = "table"
)

func TestChunksPartialError(t *testing.T) {
	fixture := dynamoDBFixture(0, 10, 20)
	_, client, closer, err := testutils.Setup(fixture, tableName)
	require.NoError(t, err)
	defer closer.Close()

	sc, ok := client.(*dynamoDBStorageClient)
	if !ok {
		t.Error("DynamoDB test client has unexpected type")
		return
	}
	ctx := context.Background()
	// Create more chunks than we can read in one batch
	s := chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			{
				From:      chunk.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}
	_, chunks, err := testutils.CreateChunks(s, 0, dynamoDBMaxReadBatchSize+50, model.Now().Add(-time.Hour), model.Now())
	require.NoError(t, err)
	err = client.PutChunks(ctx, chunks)
	require.NoError(t, err)

	// Make the read fail after 1 success, and keep failing until all retries are exhausted
	sc.setErrorParameters(999, 1)
	// Try to read back all the chunks we created, so we should get an error plus the first batch
	chunksWeGot, err := client.GetChunks(ctx, chunks)
	require.Error(t, err)
	require.Equal(t, dynamoDBMaxReadBatchSize, len(chunksWeGot))
}
