package storage

import (
	"context"
	"encoding/hex"
	"hash/fnv"
	"sync"
	"time"

	proto "github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
	chunk_util "github.com/weaveworks/cortex/pkg/chunk/util"
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

func hashKey(key string) string {
	hasher := fnv.New64a()
	hasher.Write([]byte(key)) // This'll never error.

	// Hex because memcache errors for the bytes produced by the hash.
	return hex.EncodeToString(hasher.Sum(nil))
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

func (s *cachingStorageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	// We cache the entire row, so filter client side.
	callback = chunk_util.QueryFilter(callback)
	cacheableMissed := []chunk.IndexQuery{}
	missed := map[string]chunk.IndexQuery{}

	for _, query := range queries {
		key := queryKey(query)
		batch, ok, err := s.cache.Fetch(ctx, key)
		if err != nil {
			cacheCorruptErrs.Inc()
		} else if ok {
			callback(query, batch)
			continue
		}

		// Just reads the entire row and caches it; filter client side.
		cacheableMissed = append(cacheableMissed, chunk.IndexQuery{
			TableName: query.TableName,
			HashValue: query.HashValue,
		})
		missed[key] = query
	}

	if len(cacheableMissed) == 0 {
		return nil
	}

	var resultsMtx sync.Mutex
	results := map[string]ReadBatch{}
	expiryTime := time.Now().Add(s.validity)
	err := s.StorageClient.QueryPages(ctx, cacheableMissed, func(cacheableQuery chunk.IndexQuery, r chunk.ReadBatch) bool {
		resultsMtx.Lock()
		defer resultsMtx.Unlock()
		key := queryKey(cacheableQuery)
		existing, ok := results[key]
		if !ok {
			existing = ReadBatch{
				Key:    key,
				Expiry: expiryTime.UnixNano(),
			}
		}
		for iter := r.Iterator(); iter.Next(); {
			existing.Entries = append(existing.Entries, Entry{Column: iter.RangeValue(), Value: iter.Value()})
		}
		results[key] = existing
		return true
	})
	if err != nil {
		return err
	}

	resultsMtx.Lock()
	defer resultsMtx.Unlock()
	for key, batch := range results {
		query := missed[key]
		callback(query, batch)
		s.cache.Store(ctx, queryKey(query), batch)
	}
	return nil
}

// Iterator implements chunk.ReadBatch.
func (b ReadBatch) Iterator() chunk.ReadBatchIterator {
	return &readBatchIterator{
		index:     -1,
		readBatch: b,
	}
}

type readBatchIterator struct {
	index     int
	readBatch ReadBatch
}

// Len implements chunk.ReadBatchIterator.
func (b *readBatchIterator) Next() bool {
	b.index++
	return b.index < len(b.readBatch.Entries)
}

// RangeValue implements chunk.ReadBatchIterator.
func (b *readBatchIterator) RangeValue() []byte {
	return b.readBatch.Entries[b.index].Column
}

// Value implements chunk.ReadBatchIterator.
func (b *readBatchIterator) Value() []byte {
	return b.readBatch.Entries[b.index].Value
}

func queryKey(q chunk.IndexQuery) string {
	const sep = "\xff"
	return q.TableName + sep + q.HashValue
}
