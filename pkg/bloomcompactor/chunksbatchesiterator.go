package bloomcompactor

import (
	"context"
	"errors"

	"github.com/grafana/loki/pkg/storage/chunk"
)

type chunksBatchesIterator struct {
	context          context.Context
	client           chunkClient
	chunksToDownload []chunk.Chunk
	batchSize        int

	currentBatch []chunk.Chunk
	err          error
}

func newChunkBatchesIterator(context context.Context, client chunkClient, chunksToDownload []chunk.Chunk, batchSize int) (*chunksBatchesIterator, error) {
	if batchSize <= 0 {
		return nil, errors.New("batchSize must be greater than 0")
	}
	return &chunksBatchesIterator{context: context, client: client, chunksToDownload: chunksToDownload, batchSize: batchSize}, nil
}

func (c *chunksBatchesIterator) Next() bool {
	if len(c.chunksToDownload) == 0 {
		return false
	}
	batchSize := c.batchSize
	chunksToDownloadCount := len(c.chunksToDownload)
	if chunksToDownloadCount < batchSize {
		batchSize = chunksToDownloadCount
	}
	chunksToDownload := c.chunksToDownload[:batchSize]
	c.chunksToDownload = c.chunksToDownload[batchSize:]
	c.currentBatch, c.err = c.client.GetChunks(c.context, chunksToDownload)
	return c.err == nil
}

func (c *chunksBatchesIterator) Err() error {
	return c.err
}

func (c *chunksBatchesIterator) At() []chunk.Chunk {
	return c.currentBatch
}
