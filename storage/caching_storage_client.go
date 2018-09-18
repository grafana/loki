package storage

import (
	"context"
	"encoding/hex"
	"hash/fnv"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	proto "github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
	chunk_util "github.com/weaveworks/cortex/pkg/chunk/util"
	"github.com/weaveworks/cortex/pkg/util"
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
	Fetch(ctx context.Context, keys []string) (batches []ReadBatch, misses []string)
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
	c.Cache.Store(ctx, []string{hashKey(key)}, [][]byte{out})
	return
}

func (c *indexCache) Fetch(ctx context.Context, keys []string) (batches []ReadBatch, missed []string) {
	cacheGets.Inc()

	// Build a map from hash -> key; NB there can be collisions here; we'll fetch
	// the last hash.
	hashedKeys := make(map[string]string, len(keys))
	for _, key := range keys {
		hashedKeys[hashKey(key)] = key
	}

	// Build a list of hashes; could be less than keys due to collisions.
	hashes := make([]string, 0, len(keys))
	for hash := range hashedKeys {
		hashes = append(hashes, hash)
	}

	// Look up the hashes in a single batch.  If we get an error, we just "miss" all
	// of the keys.  Eventually I want to push all the errors to the leafs of the cache
	// tree, to the caches only return found & missed.
	foundHashes, bufs, _ := c.Cache.Fetch(ctx, hashes)

	// Reverse the hash, unmarshal the index entries, check we got what we expected
	// and that its still valid.
	batches = make([]ReadBatch, 0, len(foundHashes))
	for j, foundHash := range foundHashes {
		key := hashedKeys[foundHash]
		var readBatch ReadBatch

		if err := proto.Unmarshal(bufs[j], &readBatch); err != nil {
			level.Warn(util.Logger).Log("msg", "error unmarshalling index entry from cache", "err", err)
			cacheCorruptErrs.Inc()
			continue
		}

		// Make sure the hash(key) is not a collision in the cache by looking at the
		// key in the value.
		if key != readBatch.Key || time.Now().After(time.Unix(0, readBatch.Expiry)) {
			cacheCorruptErrs.Inc()
			continue
		}

		cacheHits.Inc()
		batches = append(batches, readBatch)
	}

	// Finally work out what we're missing.
	misses := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		misses[key] = struct{}{}
	}
	for i := range batches {
		delete(misses, batches[i].Key)
	}
	missed = make([]string, 0, len(misses))
	for miss := range misses {
		missed = append(missed, miss)
	}

	return batches, missed
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

	// Build list of keys to lookup in the cache.
	keys := make([]string, 0, len(queries))
	queriesByKey := make(map[string][]chunk.IndexQuery, len(queries))
	for _, query := range queries {
		key := queryKey(query)
		keys = append(keys, key)
		queriesByKey[key] = append(queriesByKey[key], query)
	}

	batches, misses := s.cache.Fetch(ctx, keys)
	for _, batch := range batches {
		queries := queriesByKey[batch.Key]
		for _, query := range queries {
			callback(query, batch)
		}
	}

	if len(misses) == 0 {
		return nil
	}

	// Build list of cachable queries for the queries that missed the cache.
	cacheableMissed := []chunk.IndexQuery{}
	for _, key := range misses {
		// Only need to consider one of the queries as they have the same table & hash.
		queries := queriesByKey[key]
		cacheableMissed = append(cacheableMissed, chunk.IndexQuery{
			TableName: queries[0].TableName,
			HashValue: queries[0].HashValue,
		})
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
		queries := queriesByKey[key]
		for _, query := range queries {
			callback(query, batch)
		}
		s.cache.Store(ctx, key, batch)
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
