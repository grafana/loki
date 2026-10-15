package cache

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type statsCollector[K comparable, V any] struct {
	TypedCache[K, V]
	valueSize func(V) int
}

// CollectStats returns a new Cache that keeps various statistics on cache usage.
func CollectStats(cache Cache) Cache {
	return CollectTypedStats[string, []byte](cache, byteSliceSize)
}

// CollectTypedStats returns a new typed cache that keeps the same query
// statistics as CollectStats. valueSize reports the size of a cached value in bytes.
func CollectTypedStats[K comparable, V any](cache TypedCache[K, V], valueSize func(V) int) TypedCache[K, V] {
	return statsCollector[K, V]{
		TypedCache: cache,
		valueSize:  valueSize,
	}
}

func (s statsCollector[K, V]) Store(ctx context.Context, keys []K, values []V) error {
	st := stats.FromContext(ctx)
	st.AddCacheRequest(s.TypedCache.GetCacheType(), 1)

	// we blindly count the number of keys to be stored since we can't know if these will actually be written back to
	// the cache successfully if cache.backgroundCache is in use
	st.AddCacheEntriesStored(s.TypedCache.GetCacheType(), len(keys))

	return s.TypedCache.Store(ctx, keys, values)
}

func (s statsCollector[K, V]) Fetch(ctx context.Context, keys []K) (found []K, values []V, missing []K, err error) {
	st := stats.FromContext(ctx)
	st.AddCacheRequest(s.TypedCache.GetCacheType(), 1)

	start := time.Now()

	found, values, missing, err = s.TypedCache.Fetch(ctx, keys)

	st.AddCacheDownloadTime(s.TypedCache.GetCacheType(), time.Since(start))
	st.AddCacheEntriesFound(s.TypedCache.GetCacheType(), len(found))
	st.AddCacheEntriesRequested(s.TypedCache.GetCacheType(), len(keys))

	for j := range values {
		st.AddCacheBytesRetrieved(s.TypedCache.GetCacheType(), s.valueSize(values[j]))
	}

	return found, values, missing, err
}

func (s statsCollector[K, V]) Stop() {
	s.TypedCache.Stop()
}

func (s statsCollector[K, V]) GetCacheType() stats.CacheType {
	return s.TypedCache.GetCacheType()
}
