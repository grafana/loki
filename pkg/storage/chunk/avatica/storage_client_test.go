package avatica

import (
	"context"
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/storage/chunk"

	"github.com/stretchr/testify/require"
)

func TestIndexWrite(t *testing.T) {
	if cfg.Addresses == "" { //skip test
		return
	}
	ctx := context.Background()
	var client chunk.IndexClient
	client, err := NewStorageClient(cfg, registerer)
	require.NoError(t, err)

	// Write out 30 entries, into different hash and range values.
	batch := client.NewWriteBatch()
	for i := 0; i < 30; i++ {
		batch.Add(testTableName, fmt.Sprintf("hash%d", i), []byte(fmt.Sprintf("range%d", i)), []byte(fmt.Sprintf("value%d", i)))
	}
	err = client.BatchWrite(ctx, batch)
	require.NoError(t, err)
}

func TestIndexRead(t *testing.T) {
	if cfg.Addresses == "" { //skip test
		return
	}
	ctx := context.Background()
	var client chunk.IndexClient
	client, err := NewStorageClient(cfg, registerer)
	require.NoError(t, err)
	// Make sure we get back the correct entries by hash value.
	for i := 0; i < 30; i++ {
		entries := []chunk.IndexQuery{
			{
				TableName: testTableName,
				HashValue: fmt.Sprintf("hash%d", i),
			},
		}
		var have []chunk.IndexEntry
		err := client.QueryPages(ctx, entries, func(_ chunk.IndexQuery, read chunk.ReadBatch) bool {
			iter := read.Iterator()
			for iter.Next() {
				have = append(have, chunk.IndexEntry{
					RangeValue: iter.RangeValue(),
				})
			}
			return true
		})
		require.NoError(t, err)
		require.Equal(t, []chunk.IndexEntry{
			{RangeValue: []byte(fmt.Sprintf("range%d", i))},
		}, have)
	}
}
