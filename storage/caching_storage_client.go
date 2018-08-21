package storage

import (
	"bytes"
	"context"
	"strings"
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
		cache:         chunk.NewFifoCache("index", size, validity),
		validity:      validity,
	}
}

func (s *cachingStorageClient) QueryPages(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	value, ok := s.cache.Get(queryKey(query))
	if ok {
		batches := value.([]chunk.ReadBatch)
		filteredBatch := filterBatchByQuery(query, batches)
		callback(filteredBatch)

		return nil
	}

	batches := []chunk.ReadBatch{}
	cacheableQuery := chunk.IndexQuery{
		TableName: query.TableName,
		HashValue: query.HashValue,
	} // Just reads the entire row and caches it.

	err := s.StorageClient.QueryPages(ctx, cacheableQuery, copyingCallback(&batches))
	if err != nil {
		return err
	}

	filteredBatch := filterBatchByQuery(query, batches)
	callback(filteredBatch)

	s.cache.Put(queryKey(query), batches)

	return nil
}

type readBatch []cell

func (b readBatch) Len() int                { return len(b) }
func (b readBatch) RangeValue(i int) []byte { return b[i].column }
func (b readBatch) Value(i int) []byte      { return b[i].value }

type cell struct {
	column []byte
	value  []byte
}

func copyingCallback(readBatches *[]chunk.ReadBatch) func(chunk.ReadBatch) bool {
	return func(result chunk.ReadBatch) bool {
		*readBatches = append(*readBatches, result)
		return true
	}
}

func queryKey(q chunk.IndexQuery) string {
	const sep = "\xff"
	return q.TableName + sep + q.HashValue
}

func filterBatchByQuery(query chunk.IndexQuery, batches []chunk.ReadBatch) readBatch {
	var filter func([]byte, []byte) bool

	if len(query.RangeValuePrefix) != 0 {
		filter = func(rangeValue []byte, value []byte) bool {
			return strings.HasPrefix(string(rangeValue), string(query.RangeValuePrefix))
		}
	}
	if len(query.RangeValueStart) != 0 {
		filter = func(rangeValue []byte, value []byte) bool {
			return string(rangeValue) >= string(query.RangeValueStart)
		}
	}
	if len(query.ValueEqual) != 0 {
		// This is on top of the existing filters.
		existingFilter := filter
		filter = func(rangeValue []byte, value []byte) bool {
			return existingFilter(rangeValue, value) && bytes.Equal(value, query.ValueEqual)
		}
	}

	finalBatch := make(readBatch, 0, len(batches)) // On the higher side for most queries. On the lower side for column key schema.
	for _, batch := range batches {
		for i := 0; i < batch.Len(); i++ {
			if filter(batch.RangeValue(i), batch.Value(i)) {
				finalBatch = append(finalBatch, cell{column: batch.RangeValue(i), value: batch.Value(i)})
			}
		}
	}

	return finalBatch
}
