package storage

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/aws"
	"github.com/weaveworks/cortex/pkg/chunk/cassandra"
	"github.com/weaveworks/cortex/pkg/chunk/gcp"
	promchunk "github.com/weaveworks/cortex/pkg/prom1/storage/local/chunk"
)

const (
	userID    = "userID"
	tableName = "test"
)

type storageClientTest func(*testing.T, chunk.StorageClient)

func forAllFixtures(t *testing.T, storageClientTest storageClientTest) {
	fixtures := append(aws.Fixtures, gcp.Fixtures...)

	cassandraFixtures, err := cassandra.Fixtures()
	require.NoError(t, err)
	fixtures = append(fixtures, cassandraFixtures...)

	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			storageClient, tableClient, schemaConfig, err := fixture.Clients()
			require.NoError(t, err)
			defer fixture.Teardown()

			tableManager, err := chunk.NewTableManager(schemaConfig, 12*time.Hour, tableClient)
			require.NoError(t, err)

			err = tableManager.SyncTables(context.Background())
			require.NoError(t, err)

			err = tableClient.CreateTable(context.Background(), chunk.TableDesc{
				Name: tableName,
			})
			require.NoError(t, err)

			storageClientTest(t, storageClient)
		})
	}
}

func dummyChunk(now model.Time) chunk.Chunk {
	return dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
	})
}

func dummyChunkFor(now model.Time, metric model.Metric) chunk.Chunk {
	cs, _ := promchunk.New().Add(model.SamplePair{Timestamp: now, Value: 0})
	chunk := chunk.NewChunk(
		userID,
		metric.Fingerprint(),
		metric,
		cs[0],
		now.Add(-time.Hour),
		now,
	)
	// Force checksum calculation.
	_, err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}
