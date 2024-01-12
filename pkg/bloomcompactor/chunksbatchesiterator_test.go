package bloomcompactor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_chunksBatchesIterator(t *testing.T) {
	tests := map[string]struct {
		batchSize        int
		chunksToDownload []chunk.Chunk
		constructorError error

		hadNextCount int
	}{
		"expected error if batch size is set to 0": {
			batchSize:        0,
			constructorError: errors.New("batchSize must be greater than 0"),
		},
		"expected no error if there are no chunks": {
			hadNextCount: 0,
			batchSize:    10,
		},
		"expected 1 call to the client": {
			chunksToDownload: createFakeChunks(10),
			hadNextCount:     1,
			batchSize:        20,
		},
		"expected 1 call to the client(2)": {
			chunksToDownload: createFakeChunks(10),
			hadNextCount:     1,
			batchSize:        10,
		},
		"expected 2 calls to the client": {
			chunksToDownload: createFakeChunks(10),
			hadNextCount:     2,
			batchSize:        6,
		},
		"expected 10 calls to the client": {
			chunksToDownload: createFakeChunks(10),
			hadNextCount:     10,
			batchSize:        1,
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{}
			iterator, err := newChunkBatchesIterator(context.Background(), client, data.chunksToDownload, data.batchSize)
			if data.constructorError != nil {
				require.Equal(t, err, data.constructorError)
				return
			}
			hadNextCount := 0
			var downloadedChunks []chunk.Chunk
			for iterator.Next() {
				hadNextCount++
				downloaded := iterator.At()
				downloadedChunks = append(downloadedChunks, downloaded...)
				require.LessOrEqual(t, len(downloaded), data.batchSize)
			}
			require.NoError(t, iterator.Err())
			require.Equal(t, data.chunksToDownload, downloadedChunks)
			require.Equal(t, data.hadNextCount, client.callsCount)
			require.Equal(t, data.hadNextCount, hadNextCount)
		})
	}
}

func createFakeChunks(count int) []chunk.Chunk {
	metas := make([]tsdbindex.ChunkMeta, 0, count)
	for i := 0; i < count; i++ {
		metas = append(metas, tsdbindex.ChunkMeta{
			Checksum: uint32(i),
			MinTime:  int64(i),
			MaxTime:  int64(i + 100),
			KB:       uint32(i * 100),
			Entries:  uint32(i * 10),
		})
	}
	return makeChunkRefs(metas, "fake", 0xFFFF)
}

type fakeClient struct {
	callsCount int
}

func (f *fakeClient) GetChunks(_ context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	f.callsCount++
	return chunks, nil
}
