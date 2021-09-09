package storage

import (
	"context"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
)

var (
	cacheCorruptErrs = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "querier_index_cache_corruptions_total",
		Help:      "The number of cache corruptions for the index cache.",
	})
	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "querier_index_cache_hits_total",
		Help:      "The number of cache hits for the index cache.",
	})
	cacheGets = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "querier_index_cache_gets_total",
		Help:      "The number of gets for the index cache.",
	})
	cachePuts = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "querier_index_cache_puts_total",
		Help:      "The number of puts for the index cache.",
	})
	cacheEncodeErrs = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "querier_index_cache_encode_errors_total",
		Help:      "The number of errors for the index cache while encoding the body.",
	})
)

const sep = "\xff"

type cachingIndexClient struct {
	chunk.IndexClient
	cache               cache.Cache
	validity            time.Duration
	limits              StoreLimits
	logger              log.Logger
	disableBroadQueries bool
}

func newCachingIndexClient(client chunk.IndexClient, c cache.Cache, validity time.Duration, limits StoreLimits, logger log.Logger, disableBroadQueries bool) chunk.IndexClient {
	if c == nil || cache.IsEmptyTieredCache(c) {
		return client
	}

	return &cachingIndexClient{
		IndexClient:         client,
		cache:               cache.NewSnappy(c, logger),
		validity:            validity,
		limits:              limits,
		logger:              logger,
		disableBroadQueries: disableBroadQueries,
	}
}

func (s *cachingIndexClient) Stop() {
	s.cache.Stop()
	s.IndexClient.Stop()
}

func (s *cachingIndexClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	if len(queries) == 0 {
		return nil
	}

	if isChunksQuery(queries[0]) || !s.disableBroadQueries {
		return s.doBroadQueries(ctx, queries, callback)
	}

	return s.doQueries(ctx, queries, callback)
}

func (s *cachingIndexClient) queryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback,
	buildIndexQuery func(query chunk.IndexQuery) chunk.IndexQuery, buildQueryKey func(query chunk.IndexQuery) string) error {
	if len(queries) == 0 {
		return nil
	}

	userID, err := tenant.ID(ctx)
	if err != nil {
		return err
	}
	cardinalityLimit := int32(s.limits.CardinalityLimit(userID))

	// Build list of keys to lookup in the cache.
	keys := make([]string, 0, len(queries))
	queriesByKey := make(map[string][]chunk.IndexQuery, len(queries))
	for _, query := range queries {
		key := buildQueryKey(query)
		keys = append(keys, key)
		queriesByKey[key] = append(queriesByKey[key], query)
	}

	batches, misses := s.cacheFetch(ctx, keys)
	for _, batch := range batches {
		if cardinalityLimit > 0 && batch.Cardinality > cardinalityLimit {
			return chunk.CardinalityExceededError{
				Size:  batch.Cardinality,
				Limit: cardinalityLimit,
			}
		}

		queries := queriesByKey[batch.Key]
		for _, query := range queries {
			callback(query, batch)
		}
	}

	if len(misses) == 0 {
		return nil
	}

	// Build list of cachable queries for the queries that missed the cache.
	var (
		resultsMtx      sync.Mutex
		results         = make(map[string]ReadBatch, len(misses))
		cacheableMissed = make([]chunk.IndexQuery, 0, len(misses))
		expiryTime      = time.Now().Add(s.validity)
	)

	for _, key := range misses {
		queries := queriesByKey[key]
		// queries with the same key would build same index query so just consider one of them
		cacheableMissed = append(cacheableMissed, buildIndexQuery(queries[0]))

		rb := ReadBatch{
			Key:    key,
			Expiry: expiryTime.UnixNano(),
		}

		// If the query is cacheable forever, nil the expiry.
		if queries[0].Immutable {
			rb.Expiry = 0
		}

		results[key] = rb
	}

	err = s.IndexClient.QueryPages(ctx, cacheableMissed, func(cacheableQuery chunk.IndexQuery, r chunk.ReadBatch) bool {
		resultsMtx.Lock()
		defer resultsMtx.Unlock()
		key := buildQueryKey(cacheableQuery)
		existing := results[key]
		for iter := r.Iterator(); iter.Next(); {
			existing.Entries = append(existing.Entries, Entry{Column: iter.RangeValue(), Value: iter.Value()})
		}
		results[key] = existing
		return true
	})
	if err != nil {
		return err
	}

	{
		resultsMtx.Lock()
		defer resultsMtx.Unlock()
		keys := make([]string, 0, len(results))
		batches := make([]ReadBatch, 0, len(results))
		var cardinalityErr error
		for key, batch := range results {
			cardinality := int32(len(batch.Entries))
			if cardinalityLimit > 0 && cardinality > cardinalityLimit {
				batch.Cardinality = cardinality
				batch.Entries = nil
				cardinalityErr = chunk.CardinalityExceededError{
					Size:  cardinality,
					Limit: cardinalityLimit,
				}
			}

			keys = append(keys, key)
			batches = append(batches, batch)
			if cardinalityErr != nil {
				continue
			}

			queries := queriesByKey[key]
			for _, query := range queries {
				callback(query, batch)
			}
		}
		s.cacheStore(ctx, keys, batches)
		return cardinalityErr
	}
}

// doBroadQueries does broad queries on the store by using just TableName and HashValue.
// This is useful for chunks queries or when we need to reduce QPS on index store at the expense of higher cache requirement.
// All the results from the index store are cached and the responses are filtered based on the actual queries.
func (s *cachingIndexClient) doBroadQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	// We cache all the entries for queries looking for Chunk IDs, so filter client side.
	callback = chunk_util.QueryFilter(callback)
	return s.queryPages(ctx, queries, callback, func(query chunk.IndexQuery) chunk.IndexQuery {
		return chunk.IndexQuery{TableName: query.TableName, HashValue: query.HashValue}
	}, func(q chunk.IndexQuery) string {
		return q.TableName + sep + q.HashValue
	})
}

// doQueries does the exact same queries as opposed to doBroadQueries doing broad queries with limited query params.
func (s *cachingIndexClient) doQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	return s.queryPages(ctx, queries, callback, func(query chunk.IndexQuery) chunk.IndexQuery {
		return query
	}, func(q chunk.IndexQuery) string {
		ret := q.TableName + sep + q.HashValue

		if len(q.RangeValuePrefix) != 0 {
			ret += sep + yoloString(q.RangeValuePrefix)
		}

		if len(q.ValueEqual) != 0 {
			ret += sep + yoloString(q.ValueEqual)
		}

		return ret
	})
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

func isChunksQuery(q chunk.IndexQuery) bool {
	// RangeValueStart would only be set for chunks query.
	return len(q.RangeValueStart) != 0
}

func (s *cachingIndexClient) cacheStore(ctx context.Context, keys []string, batches []ReadBatch) {
	cachePuts.Add(float64(len(keys)))

	// We're doing the hashing to handle unicode and key len properly.
	// Memcache fails for unicode keys and keys longer than 250 Bytes.
	hashed := make([]string, 0, len(keys))
	bufs := make([][]byte, 0, len(batches))
	for i := range keys {
		hashed = append(hashed, cache.HashKey(keys[i]))
		out, err := proto.Marshal(&batches[i])
		if err != nil {
			level.Warn(s.logger).Log("msg", "error marshalling ReadBatch", "err", err)
			cacheEncodeErrs.Inc()
			return
		}
		bufs = append(bufs, out)
	}

	s.cache.Store(ctx, hashed, bufs)
}

func (s *cachingIndexClient) cacheFetch(ctx context.Context, keys []string) (batches []ReadBatch, missed []string) {
	log, ctx := spanlogger.New(ctx, "cachingIndexClient.cacheFetch")
	defer log.Finish()

	cacheGets.Add(float64(len(keys)))

	// Build a map from hash -> key; NB there can be collisions here; we'll fetch
	// the last hash.
	hashedKeys := make(map[string]string, len(keys))
	for _, key := range keys {
		hashedKeys[cache.HashKey(key)] = key
	}

	// Build a list of hashes; could be less than keys due to collisions.
	hashes := make([]string, 0, len(keys))
	for hash := range hashedKeys {
		hashes = append(hashes, hash)
	}

	// Look up the hashes in a single batch.  If we get an error, we just "miss" all
	// of the keys.  Eventually I want to push all the errors to the leafs of the cache
	// tree, to the caches only return found & missed.
	foundHashes, bufs, _ := s.cache.Fetch(ctx, hashes)

	// Reverse the hash, unmarshal the index entries, check we got what we expected
	// and that its still valid.
	batches = make([]ReadBatch, 0, len(foundHashes))
	for j, foundHash := range foundHashes {
		key := hashedKeys[foundHash]
		var readBatch ReadBatch

		if err := proto.Unmarshal(bufs[j], &readBatch); err != nil {
			level.Warn(log).Log("msg", "error unmarshalling index entry from cache", "err", err)
			cacheCorruptErrs.Inc()
			continue
		}

		// Make sure the hash(key) is not a collision in the cache by looking at the
		// key in the value.
		if key != readBatch.Key {
			level.Debug(log).Log("msg", "dropping index cache entry due to key collision", "key", key, "readBatch.Key", readBatch.Key, "expiry")
			continue
		}

		if readBatch.Expiry != 0 && time.Now().After(time.Unix(0, readBatch.Expiry)) {
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

	level.Debug(log).Log("hits", len(batches), "misses", len(misses))
	return batches, missed
}
