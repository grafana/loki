package aws

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

const (
	tableName = "table"
)

func TestChunksPartialError(t *testing.T) {
	fixture := dynamoDBFixture(0, 10, 20)
	_, c, closer, err := testutils.Setup(fixture, tableName)
	require.NoError(t, err)
	defer closer.Close()

	sc, ok := c.(*dynamoDBStorageClient)
	if !ok {
		t.Error("DynamoDB test client has unexpected type")
		return
	}
	ctx := context.Background()
	// Create more chunks than we can read in one batch
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}
	_, chunks, err := testutils.CreateChunks(s, 0, dynamoDBMaxReadBatchSize+50, model.Now().Add(-time.Hour), model.Now())
	require.NoError(t, err)
	err = sc.PutChunks(ctx, chunks)
	require.NoError(t, err)

	// Make the read fail after 1 success, and keep failing until all retries are exhausted
	sc.setErrorParameters(999, 1)
	// Try to read back all the chunks we created, so we should get an error plus the first batch
	chunksWeGot, err := sc.GetChunks(ctx, chunks)
	require.Error(t, err)
	require.Equal(t, dynamoDBMaxReadBatchSize, len(chunksWeGot))
}

func TestNilConsumedCapacity(t *testing.T) {
	fixture := dynamoDBFixture(0, 10, 20)
	indexClient, chunkClient, closer, err := testutils.Setup(fixture, tableName)
	require.NoError(t, err)
	defer closer.Close()

	sc, ok := chunkClient.(*dynamoDBStorageClient)
	if !ok {
		t.Error("DynamoDB test client has unexpected type")
		return
	}

	sc.setConsumedCapacityRefs(consumedCapacityNilRefs)

	ctx := context.Background()
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	t.Run("BatchWrite with nil ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 10, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)
	})

	t.Run("Query with nil ConsumedCapacity", func(t *testing.T) {
		batch := indexClient.NewWriteBatch()
		batch.Add(tableName, "hash1", []byte("range1"), []byte("value1"))
		err := indexClient.BatchWrite(ctx, batch)
		require.NoError(t, err)

		queries := []index.Query{
			{
				TableName: tableName,
				HashValue: "hash1",
			},
		}

		var callbackInvoked bool
		err = indexClient.QueryPages(ctx, queries, func(query index.Query, resp index.ReadBatchResult) bool {
			callbackInvoked = true
			return true
		})
		require.NoError(t, err)
		require.True(t, callbackInvoked, "callback should have been invoked")
	})

	t.Run("BatchGetItem with nil ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 5, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)

		retrievedChunks, err := sc.GetChunks(ctx, chunks)
		require.NoError(t, err)
		require.Equal(t, len(chunks), len(retrievedChunks))
	})
}

func TestNoConsumedCapacity(t *testing.T) {
	fixture := dynamoDBFixture(0, 10, 20)
	indexClient, chunkClient, closer, err := testutils.Setup(fixture, tableName)
	require.NoError(t, err)
	defer closer.Close()

	sc, ok := chunkClient.(*dynamoDBStorageClient)
	if !ok {
		t.Error("DynamoDB test client has unexpected type")
		return
	}

	sc.setConsumedCapacityRefs(consumedCapacityNone)

	ctx := context.Background()
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	t.Run("BatchWrite with no ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 10, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)
	})

	t.Run("Query with no ConsumedCapacity", func(t *testing.T) {
		batch := indexClient.NewWriteBatch()
		batch.Add(tableName, "hash1", []byte("range1"), []byte("value1"))
		err := indexClient.BatchWrite(ctx, batch)
		require.NoError(t, err)

		queries := []index.Query{
			{
				TableName: tableName,
				HashValue: "hash1",
			},
		}

		var callbackInvoked bool
		err = indexClient.QueryPages(ctx, queries, func(query index.Query, resp index.ReadBatchResult) bool {
			callbackInvoked = true
			return true
		})
		require.NoError(t, err)
		require.True(t, callbackInvoked, "callback should have been invoked")
	})

	t.Run("BatchGetItem with no ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 5, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)

		retrievedChunks, err := sc.GetChunks(ctx, chunks)
		require.NoError(t, err)
		require.Equal(t, len(chunks), len(retrievedChunks))
	})
}

func TestValidConsumedCapacity(t *testing.T) {
	fixture := dynamoDBFixture(0, 10, 20)
	indexClient, chunkClient, closer, err := testutils.Setup(fixture, tableName)
	require.NoError(t, err)
	defer closer.Close()

	sc, ok := chunkClient.(*dynamoDBStorageClient)
	if !ok {
		t.Error("DynamoDB test client has unexpected type")
		return
	}

	sc.setConsumedCapacityRefs(consumedCapacityValidRefs)

	ctx := context.Background()
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	t.Run("BatchWrite with valid ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 10, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)
	})

	t.Run("Query with valid ConsumedCapacity", func(t *testing.T) {
		batch := indexClient.NewWriteBatch()
		batch.Add(tableName, "hash1", []byte("range1"), []byte("value1"))
		err := indexClient.BatchWrite(ctx, batch)
		require.NoError(t, err)

		queries := []index.Query{
			{
				TableName: tableName,
				HashValue: "hash1",
			},
		}

		var callbackInvoked bool
		err = indexClient.QueryPages(ctx, queries, func(query index.Query, resp index.ReadBatchResult) bool {
			callbackInvoked = true
			return true
		})
		require.NoError(t, err)
		require.True(t, callbackInvoked, "callback should have been invoked")
	})

	t.Run("BatchGetItem with valid ConsumedCapacity", func(t *testing.T) {
		_, chunks, err := testutils.CreateChunks(s, 0, 5, model.Now().Add(-time.Hour), model.Now())
		require.NoError(t, err)

		err = sc.PutChunks(ctx, chunks)
		require.NoError(t, err)

		retrievedChunks, err := sc.GetChunks(ctx, chunks)
		require.NoError(t, err)
		require.Equal(t, len(chunks), len(retrievedChunks))
	})
}
