package storage

import (
	"context"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v2/pkg/storage/chunk"
	"github.com/grafana/loki/v2/pkg/storage/chunk/testutils"
)

func TestChunksBasic(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, _ chunk.IndexClient, client chunk.Client) {
		s := chunk.SchemaConfig{
			Configs: []chunk.PeriodConfig{
				{
					From:      chunk.DayTime{Time: 0},
					Schema:    "v11",
					RowShards: 16,
				},
			},
		}
		const batchSize = 5
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		// Write a few batches of chunks.
		written := []string{}
		for i := 0; i < 5; i++ {
			keys, chunks, err := testutils.CreateChunks(s, i, batchSize, model.Now().Add(-time.Hour), model.Now())
			require.NoError(t, err)
			written = append(written, keys...)
			err = client.PutChunks(ctx, chunks)
			require.NoError(t, err)
		}

		// Get a few batches of chunks.
		for batch := 0; batch < 50; batch++ {
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

			sort.Sort(ByKey{chunksToGet, s})
			sort.Sort(ByKey{chunksWeGot, s})
			for i := 0; i < len(chunksWeGot); i++ {
				require.Equal(t, s.ExternalKey(chunksToGet[i]), s.ExternalKey(chunksWeGot[i]), strconv.Itoa(i))
			}
		}
	})
}
