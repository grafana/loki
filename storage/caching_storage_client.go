package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"hash/fnv"
	"strings"
	"time"

	proto "github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
)

var (
	cacheCorruptErrs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "querier_index_cache_corruptions_total",
		Help: "The number of cache corruptions for the index cache.",
	})
	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "querier_index_cache_hits_total",
		Help: "The number of cache hits for the index cache.",
	})
	cacheGets = promauto.NewCounter(prometheus.CounterOpts{
		Name: "querier_index_cache_gets_total",
		Help: "The number of gets for the index cache.",
	})
	cachePuts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "querier_index_cache_puts_total",
		Help: "The number of puts for the index cache.",
	})
	cacheEncodeErrs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "querier_index_cache_encode_errors_total",
		Help: "The number of errors for the index cache while encoding the body.",
	})
)

// IndexCache describes the cache for the Index.
type IndexCache interface {
	Store(ctx context.Context, key string, val ReadBatch)
	Fetch(ctx context.Context, key string) (val ReadBatch, ok bool, err error)
	Stop() error
}

type indexCache struct {
	cache.Cache
}

func (c *indexCache) Store(ctx context.Context, key string, val ReadBatch) {
	cachePuts.Inc()
	out, err := proto.Marshal(&val)
	if err != nil {
		cacheEncodeErrs.Inc()
		return
	}

	// We're doing the hashing to handle unicode and key len properly.
	// Memcache fails for unicode keys and keys longer than 250 Bytes.
	c.Cache.Store(ctx, hashKey(key), out)
	return
}

func (c *indexCache) Fetch(ctx context.Context, key string) (ReadBatch, bool, error) {
	cacheGets.Inc()

	found, valBytes, _, err := c.Cache.Fetch(ctx, []string{hashKey(key)})
	if len(found) != 1 || err != nil {
		return ReadBatch{}, false, err
	}

	var rb ReadBatch
	if err := proto.Unmarshal(valBytes[0], &rb); err != nil {
		return rb, false, err
	}

	// Make sure the hash(key) is not a collision by looking at the key in the value.
	if key == rb.Key && time.Now().Before(time.Unix(0, rb.Expiry)) {
		cacheHits.Inc()
		return rb, true, nil
	}

	return ReadBatch{}, false, nil
}

type cachingStorageClient struct {
	chunk.StorageClient
	cache    IndexCache
	validity time.Duration
}

func newCachingStorageClient(client chunk.StorageClient, cache cache.Cache, validity time.Duration) chunk.StorageClient {
	if cache == nil {
		return client
	}

	return &cachingStorageClient{
		StorageClient: client,
		cache:         &indexCache{cache},
		validity:      validity,
	}
}

func (s *cachingStorageClient) QueryPages(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	value, ok, err := s.cache.Fetch(ctx, queryKey(query))
	if err != nil {
		cacheCorruptErrs.Inc()
	}

	if ok && err == nil {
		filteredBatch, _ := filterBatchByQuery(query, []chunk.ReadBatch{value})
		callback(filteredBatch)

		return nil
	}

	batches := []chunk.ReadBatch{}
	cacheableQuery := chunk.IndexQuery{
		TableName: query.TableName,
		HashValue: query.HashValue,
	} // Just reads the entire row and caches it.

	expiryTime := time.Now().Add(s.validity)
	err = s.StorageClient.QueryPages(ctx, cacheableQuery, copyingCallback(&batches))
	if err != nil {
		return err
	}

	filteredBatch, totalBatches := filterBatchByQuery(query, batches)
	callback(filteredBatch)

	totalBatches.Key = queryKey(query)
	totalBatches.Expiry = expiryTime.UnixNano()

	s.cache.Store(ctx, totalBatches.Key, totalBatches)
	return nil
}

// Len implements chunk.ReadBatch.
func (b ReadBatch) Len() int { return len(b.Entries) }

// RangeValue implements chunk.ReadBatch.
func (b ReadBatch) RangeValue(i int) []byte { return b.Entries[i].Column }

// Value implements chunk.ReadBatch.
func (b ReadBatch) Value(i int) []byte { return b.Entries[i].Value }

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

func filterBatchByQuery(query chunk.IndexQuery, batches []chunk.ReadBatch) (filteredBatch, totalBatch ReadBatch) {
	filter := func([]byte, []byte) bool { return true }

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

	filteredBatch.Entries = make([]*Entry, 0, len(batches)) // On the higher side for most queries. On the lower side for column key schema.
	totalBatch.Entries = make([]*Entry, 0, len(batches))
	for _, batch := range batches {
		for i := 0; i < batch.Len(); i++ {
			totalBatch.Entries = append(totalBatch.Entries, &Entry{Column: batch.RangeValue(i), Value: batch.Value(i)})

			if filter(batch.RangeValue(i), batch.Value(i)) {
				filteredBatch.Entries = append(filteredBatch.Entries, &Entry{Column: batch.RangeValue(i), Value: batch.Value(i)})
			}
		}
	}

	return
}

func hashKey(key string) string {
	hasher := fnv.New64a()
	hasher.Write([]byte(key)) // This'll never error.

	// Hex because memcache errors for the bytes produced by the hash.
	return hex.EncodeToString(hasher.Sum(nil))
}
