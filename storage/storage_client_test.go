package storage

import (
	"context"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/weaveworks/cortex/pkg/chunk"
)

func TestChunksBasic(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client chunk.StorageClient) {
		const batchSize = 50
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Write a few batches of chunks.
		written := []string{}
		for i := 0; i < 50; i++ {
			keys, chunks, err := createChunks(i, batchSize)
			require.NoError(t, err)
			written = append(written, keys...)
			err = client.PutChunks(ctx, chunks)
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
	})
}

func createChunks(startIndex, batchSize int) ([]string, []chunk.Chunk, error) {
	keys := []string{}
	chunks := []chunk.Chunk{}
	for j := 0; j < batchSize; j++ {
		chunk := dummyChunkFor(model.Now(), model.Metric{
			model.MetricNameLabel: "foo",
			"index":               model.LabelValue(strconv.Itoa(startIndex*batchSize + j)),
		})
		chunks = append(chunks, chunk)
		_, err := chunk.Encode() // Need to encode it, side effect calculates crc
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, chunk.ExternalKey())
	}
	return keys, chunks, nil
}

type clientWithErrorParameters interface {
	SetErrorParameters(provisionedErr, errAfter int)
}

func TestChunksPartialError(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client chunk.StorageClient) {
		// This test is currently very specialised for DynamoDB
		if !strings.Contains(t.Name(), "DynamoDB") {
			return
		}
		// We use some carefully-chosen numbers:
		// Start with 150 chunks; DynamoDB writes batches in 25s so 6 batches.
		// We tell the client to error after 7 operations so all writes succeed
		// and then the 2nd read fails, so we read back only 100 chunks
		if ep, ok := client.(clientWithErrorParameters); ok {
			ep.SetErrorParameters(22, 7)
		} else {
			t.Error("DynamoDB test fixture does not support SetErrorParameters() call")
			return
		}
		ctx := context.Background()
		_, chunks, err := createChunks(0, 150)
		require.NoError(t, err)
		err = client.PutChunks(ctx, chunks)
		require.NoError(t, err)

		chunksWeGot, err := client.GetChunks(ctx, chunks)
		require.Error(t, err)
		require.Equal(t, 100, len(chunksWeGot))
	})
}
