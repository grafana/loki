package cache_test

import (
	"context"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	prom_chunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

const userID = "1"

func fillCache(t *testing.T, cache cache.Cache) ([]string, []chunk.Chunk) {
	const chunkLen = 13 * 3600 // in seconds

	// put 100 chunks from 0 to 99
	keys := []string{}
	bufs := [][]byte{}
	chunks := []chunk.Chunk{}
	for i := 0; i < 100; i++ {
		ts := model.TimeFromUnix(int64(i * chunkLen))
		promChunk, _ := prom_chunk.New().Add(model.SamplePair{
			Timestamp: ts,
			Value:     model.SampleValue(i),
		})
		c := chunk.NewChunk(
			userID,
			model.Fingerprint(1),
			labels.Labels{
				{Name: model.MetricNameLabel, Value: "foo"},
				{Name: "bar", Value: "baz"},
			},
			promChunk[0],
			ts,
			ts.Add(chunkLen),
		)

		err := c.Encode()
		require.NoError(t, err)
		buf, err := c.Encoded()
		require.NoError(t, err)

		keys = append(keys, c.ExternalKey())
		bufs = append(bufs, buf)
		chunks = append(chunks, c)
	}

	cache.Store(context.Background(), keys, bufs)
	return keys, chunks
}

func testCacheSingle(t *testing.T, cache cache.Cache, keys []string, chunks []chunk.Chunk) {
	for i := 0; i < 100; i++ {
		index := rand.Intn(len(keys))
		key := keys[index]

		found, bufs, missingKeys := cache.Fetch(context.Background(), []string{key})
		require.Len(t, found, 1)
		require.Len(t, bufs, 1)
		require.Len(t, missingKeys, 0)

		c, err := chunk.ParseExternalKey(userID, found[0])
		require.NoError(t, err)
		err = c.Decode(chunk.NewDecodeContext(), bufs[0])
		require.NoError(t, err)
		require.Equal(t, c, chunks[index])
	}
}

func testCacheMultiple(t *testing.T, cache cache.Cache, keys []string, chunks []chunk.Chunk) {
	// test getting them all
	found, bufs, missingKeys := cache.Fetch(context.Background(), keys)
	require.Len(t, found, len(keys))
	require.Len(t, bufs, len(keys))
	require.Len(t, missingKeys, 0)

	result := []chunk.Chunk{}
	for i := range found {
		c, err := chunk.ParseExternalKey(userID, found[i])
		require.NoError(t, err)
		err = c.Decode(chunk.NewDecodeContext(), bufs[i])
		require.NoError(t, err)
		result = append(result, c)
	}
	require.Equal(t, chunks, result)
}

func testChunkFetcher(t *testing.T, c cache.Cache, keys []string, chunks []chunk.Chunk) {
	fetcher, err := chunk.NewChunkFetcher(cache.Config{
		Cache: c,
	}, false, nil)
	require.NoError(t, err)
	defer fetcher.Stop()

	found, err := fetcher.FetchChunks(context.Background(), chunks, keys)
	require.NoError(t, err)
	sort.Sort(byExternalKey(found))
	sort.Sort(byExternalKey(chunks))
	require.Equal(t, chunks, found)
}

type byExternalKey []chunk.Chunk

func (a byExternalKey) Len() int           { return len(a) }
func (a byExternalKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byExternalKey) Less(i, j int) bool { return a[i].ExternalKey() < a[j].ExternalKey() }

func testCacheMiss(t *testing.T, cache cache.Cache) {
	for i := 0; i < 100; i++ {
		key := strconv.Itoa(rand.Int())
		found, bufs, missing := cache.Fetch(context.Background(), []string{key})
		require.Empty(t, found)
		require.Empty(t, bufs)
		require.Len(t, missing, 1)
	}
}

func testCache(t *testing.T, cache cache.Cache) {
	keys, chunks := fillCache(t, cache)
	t.Run("Single", func(t *testing.T) {
		testCacheSingle(t, cache, keys, chunks)
	})
	t.Run("Multiple", func(t *testing.T) {
		testCacheMultiple(t, cache, keys, chunks)
	})
	t.Run("Miss", func(t *testing.T) {
		testCacheMiss(t, cache)
	})
	t.Run("Fetcher", func(t *testing.T) {
		testChunkFetcher(t, cache, keys, chunks)
	})
}

func TestMemcache(t *testing.T) {
	t.Run("Unbatched", func(t *testing.T) {
		cache := cache.NewMemcached(cache.MemcachedConfig{}, newMockMemcache(), "test")
		testCache(t, cache)
	})

	t.Run("Batched", func(t *testing.T) {
		cache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 3,
		}, newMockMemcache(), "test")
		testCache(t, cache)
	})
}

func TestFifoCache(t *testing.T) {
	cache := cache.NewFifoCache("test", cache.FifoCacheConfig{Size: 1e3, Validity: 1 * time.Hour})
	testCache(t, cache)
}

func TestSnappyCache(t *testing.T) {
	cache := cache.NewSnappy(cache.NewMockCache())
	testCache(t, cache)
}
