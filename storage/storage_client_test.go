package storage

import (
	"context"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/aws"
	"github.com/weaveworks/cortex/pkg/chunk/gcp"
	promchunk "github.com/weaveworks/cortex/pkg/prom1/storage/local/chunk"
)

var fixtures = append(aws.Fixtures, gcp.Fixtures...)

func TestStoreChunks(t *testing.T) {
	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			storageClient, tableClient, schemaConfig, err := fixture.Clients()
			require.NoError(t, err)
			defer fixture.Teardown()

			tableManager, err := chunk.NewTableManager(schemaConfig, 12*time.Hour, tableClient)
			require.NoError(t, err)

			err = tableManager.SyncTables(context.Background())
			require.NoError(t, err)

			testStorageClientChunks(t, storageClient)
		})
	}
}

func testStorageClientChunks(t *testing.T, client chunk.StorageClient) {
	const batchSize = 50
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write a few batches of chunks.
	written := []string{}
	for i := 0; i < 50; i++ {
		chunks := []chunk.Chunk{}
		for j := 0; j < batchSize; j++ {
			chunk := dummyChunkFor(model.Now(), model.Metric{
				model.MetricNameLabel: "foo",
				"index":               model.LabelValue(strconv.Itoa(i*batchSize + j)),
			})
			chunks = append(chunks, chunk)
			_, err := chunk.Encode() // Need to encode it, side effect calculates crc
			require.NoError(t, err)
			written = append(written, chunk.ExternalKey())
		}

		err := client.PutChunks(ctx, chunks)
		require.NoError(t, err)
	}

	// Get a few batches of chunks.
	for i := 0; i < 50; i++ {
		keysToGet := map[string]struct{}{}
		chunksToGet := []chunk.Chunk{}
		for len(chunksToGet) < batchSize {
			key := written[rand.Intn(len(written))]
			if _, ok := keysToGet[key]; ok {
				continue
			}
			keysToGet[key] = struct{}{}
			chunk, err := chunk.ParseExternalKey(userID, key)
			require.NoError(t, err)
			chunksToGet = append(chunksToGet, chunk)
		}

		chunksWeGot, err := client.GetChunks(ctx, chunksToGet)
		require.NoError(t, err)
		require.Equal(t, len(chunksToGet), len(chunksWeGot))

		sort.Sort(chunk.ByKey(chunksToGet))
		sort.Sort(chunk.ByKey(chunksWeGot))
		for j := 0; j < len(chunksWeGot); j++ {
			require.Equal(t, chunksToGet[i].ExternalKey(), chunksWeGot[i].ExternalKey(), strconv.Itoa(i))
		}
	}
}

const userID = "userID"

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
