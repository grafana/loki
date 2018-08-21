package storage

import (
	"context"
	"time"

	"github.com/weaveworks/cortex/pkg/chunk"
)

type cachingStorageClient struct {
	chunk.StorageClient
	cache    *chunk.FifoCache
	validity time.Duration
}

func newCachingStorageClient(client chunk.StorageClient, size int, validity time.Duration) chunk.StorageClient {
	if size == 0 {
		return client
	}

	return &cachingStorageClient{
		StorageClient: client,
		cache:         chunk.NewFifoCache(size),
		validity:      validity,
	}
}

func (s *cachingStorageClient) QueryPages(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	value, updated, ok := s.cache.Get(queryKey(query))
	if ok && time.Now().Sub(updated) < s.validity {
		batches := value.([]chunk.ReadBatch)
		for _, batch := range batches {
			callback(batch)
		}

		return nil
	}

	readBatches := []chunk.ReadBatch{}
	err := s.StorageClient.QueryPages(ctx, query, copyingCallback(&readBatches, callback))
	if err != nil {
		return err
	}

	s.cache.Put(queryKey(query), readBatches)
	return nil
}

func copyingCallback(readBatches *[]chunk.ReadBatch, cb func(chunk.ReadBatch) bool) func(chunk.ReadBatch) bool {
	return func(result chunk.ReadBatch) bool {
		*readBatches = append(*readBatches, result)
		return cb(result)
	}
}

func queryKey(q chunk.IndexQuery) string {
	const sep = "\xff"
	return q.TableName + sep +
		q.HashValue + sep +
		string(q.RangeValuePrefix) + sep +
		string(q.RangeValueStart) + sep +
		string(q.ValueEqual)
}
