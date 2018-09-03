package storage

import (
	"context"
	"sync"
	"time"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
	chunk_util "github.com/weaveworks/cortex/pkg/chunk/util"
)

type cachingStorageClient struct {
	chunk.StorageClient
	cache    *cache.FifoCache
	validity time.Duration
}

func newCachingStorageClient(client chunk.StorageClient, size int, validity time.Duration) chunk.StorageClient {
	if size == 0 {
		return client
	}

	return &cachingStorageClient{
		StorageClient: client,
		cache:         cache.NewFifoCache("index", size, validity),
	}
}

func (s *cachingStorageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	// We cache the entire row, so filter client side.
	callback = chunk_util.QueryFilter(callback)
	cacheableMissed := []chunk.IndexQuery{}
	missed := map[string]chunk.IndexQuery{}

	for _, query := range queries {
		value, ok := s.cache.Get(ctx, queryKey(query))
		if !ok {
			cacheableMissed = append(cacheableMissed, chunk.IndexQuery{
				TableName: query.TableName,
				HashValue: query.HashValue,
			})
			missed[queryKey(query)] = query
			continue
		}

		for _, batch := range value.([]chunk.ReadBatch) {
			callback(query, batch)
		}
	}

	var resultsMtx sync.Mutex
	results := map[string][]chunk.ReadBatch{}
	err := s.StorageClient.QueryPages(ctx, cacheableMissed, func(cacheableQuery chunk.IndexQuery, r chunk.ReadBatch) bool {
		resultsMtx.Lock()
		defer resultsMtx.Unlock()
		key := queryKey(cacheableQuery)
		results[key] = append(results[key], r)
		return true
	})
	if err != nil {
		return err
	}

	resultsMtx.Lock()
	defer resultsMtx.Unlock()
	for key, batches := range results {
		query := missed[key]
		for _, batch := range batches {
			callback(query, batch)
		}
	}
	return nil
}

func queryKey(q chunk.IndexQuery) string {
	const sep = "\xff"
	return q.TableName + sep + q.HashValue
}
