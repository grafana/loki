package cache_test

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
)

const userID = "1"

func fillCache(t *testing.T, p config.PeriodConfig, cache cache.Cache) ([]string, []chunk.Chunk) {
	const chunkLen = 13 * 3600 // in seconds

	// put a set of chunks, larger than background batch size, with varying timestamps and values
	keys := []string{}
	bufs := [][]byte{}
	chunks := []chunk.Chunk{}
	for i := 0; i < 111; i++ {
		ts := model.TimeFromUnix(int64(i * chunkLen))

		cs := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncGZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)

		err := cs.Append(&logproto.Entry{
			Timestamp: ts.Time(),
			Line:      fmt.Sprintf("line ts=%d", ts),
		})
		require.NoError(t, err)
		c := chunk.NewChunk(
			userID,
			model.Fingerprint(1),
			labels.Labels{
				{Name: model.MetricNameLabel, Value: "foo"},
				{Name: "bar", Value: "baz"},
			},
			chunkenc.NewFacade(cs, 0, 0),
			ts,
			ts.Add(chunkLen),
		)

		err = c.Encode()
		require.NoError(t, err)
		buf, err := c.Encoded()
		require.NoError(t, err)

		// In order to be able to compare the expected chunk (this one) with the
		// actual one (the one that will be fetched from the cache) we need to
		// cleanup the chunk to avoid any internal references mismatch (ie. appender
		// pointer).
		cleanChunk := chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:      c.UserID,
				Fingerprint: c.Fingerprint,
				From:        c.From,
				Through:     c.Through,
				Checksum:    c.Checksum,
			},
		}
		err = cleanChunk.Decode(chunk.NewDecodeContext(), buf)
		require.NoError(t, err)

		keys = append(keys, p.ExternalKey(c.ChunkRef))
		bufs = append(bufs, buf)
		chunks = append(chunks, cleanChunk)
	}

	err := cache.Store(context.Background(), keys, bufs)
	require.NoError(t, err)
	return keys, chunks
}

func testCacheSingle(t *testing.T, cache cache.Cache, keys []string, chunks []chunk.Chunk) {
	for i := 0; i < 100; i++ {
		index := rand.Intn(len(keys))
		key := keys[index]

		found, bufs, missingKeys, _ := cache.Fetch(context.Background(), []string{key})
		require.Len(t, found, 1)
		require.Len(t, bufs, 1)
		require.Len(t, missingKeys, 0)

		c, err := chunk.ParseExternalKey(userID, found[0])
		require.NoError(t, err)
		err = c.Decode(chunk.NewDecodeContext(), bufs[0])
		require.NoError(t, err)
		require.Equal(t, chunks[index], c)
	}
}

func testCacheMultiple(t *testing.T, cache cache.Cache, keys []string, chunks []chunk.Chunk) {
	// test getting them all
	found, bufs, missingKeys, _ := cache.Fetch(context.Background(), keys)
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

func testChunkFetcher(t *testing.T, p config.PeriodConfig, c cache.Cache, chunks []chunk.Chunk) {

	fetcher, err := fetcher.New(c, nil, false, p, nil, 10, 100, 0)
	require.NoError(t, err)
	defer fetcher.Stop()

	found, err := fetcher.FetchChunks(context.Background(), chunks)
	require.NoError(t, err)
	sort.Sort(byExternalKey{found, p})
	sort.Sort(byExternalKey{chunks, p})
	require.Equal(t, chunks, found)
}

type byExternalKey struct {
	chunks []chunk.Chunk
	cfg    config.PeriodConfig
}

func (a byExternalKey) Len() int      { return len(a.chunks) }
func (a byExternalKey) Swap(i, j int) { a.chunks[i], a.chunks[j] = a.chunks[j], a.chunks[i] }
func (a byExternalKey) Less(i, j int) bool {
	return a.cfg.ExternalKey(a.chunks[i].ChunkRef) < a.cfg.ExternalKey(a.chunks[j].ChunkRef)
}

func testCacheMiss(t *testing.T, cache cache.Cache) {
	for i := 0; i < 100; i++ {
		key := strconv.Itoa(rand.Int()) // arbitrary key which should fail: no chunk key is a single integer
		found, bufs, missing, _ := cache.Fetch(context.Background(), []string{key})
		require.Empty(t, found)
		require.Empty(t, bufs)
		require.Len(t, missing, 1)
	}
}

func testCache(t *testing.T, cache cache.Cache) {
	p := config.PeriodConfig{
		From:      config.DayTime{Time: 0},
		Schema:    "v11",
		RowShards: 16,
	}

	keys, chunks := fillCache(t, p, cache)
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
		testChunkFetcher(t, p, cache, chunks)
	})
}

func TestMemcache(t *testing.T) {
	t.Run("Unbatched", func(t *testing.T) {
		cache := cache.NewMemcached(cache.MemcachedConfig{}, newMockMemcache(),
			"test", nil, log.NewNopLogger(), "test")
		testCache(t, cache)
	})

	t.Run("Batched", func(t *testing.T) {
		cache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 3,
		}, newMockMemcache(), "test", nil, log.NewNopLogger(), "test")
		testCache(t, cache)
	})
}

func TestEmbeddedCache(t *testing.T) {
	cache := cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 1e3, TTL: 1 * time.Hour},
		nil, log.NewNopLogger(), "test")
	testCache(t, cache)
}

func TestSnappyCache(t *testing.T) {
	cache := cache.NewSnappy(cache.NewMockCache(), log.NewNopLogger())
	testCache(t, cache)
}
