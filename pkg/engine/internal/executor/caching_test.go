package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var testFields = []arrow.Field{
	{Name: "val", Type: types.Arrow.String},
}

var testCacheStats = CacheStats{
	Hits:    xcap.TaskCacheHits,
	Misses:  xcap.TaskCacheMisses,
	Batches: xcap.TaskCacheBatches,
	Rows:    xcap.TaskCacheRows,
	Bytes:   xcap.TaskCacheBytes,
}

func TestCachingPipeline(t *testing.T) {
	ctx := t.Context()
	c := newMockCache()

	rec1, err := CSVToArrow(testFields, "a\nb\nc")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "d\ne")
	require.NoError(t, err)

	arrowStore := &arrowCacheAdapter{cache: c, maxSizeBytes: ^uint64(0)}

	// First pass: cache miss — inner pipeline is read and results are stored.
	miss := newCachingPipeline(arrowStore, NewBufferedPipeline(rec1, rec2), "test-key", log.NewNopLogger(), testCacheStats)
	require.NoError(t, miss.Open(ctx))
	require.False(t, miss.(*cachingPipeline).hit, "expected cache miss")

	var batches []arrow.RecordBatch
	for {
		rec, err := miss.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		batches = append(batches, rec)
	}
	miss.Close()

	require.Len(t, batches, 2, "expected 2 batches from inner pipeline")
	require.Equal(t, 1, c.setCalls, "Store should have been called once on EOF")
	require.Contains(t, c.data, cache.HashKey("test-key"), "cache should contain the key")

	// Second pass: cache hit — inner pipeline must never be opened.
	hit := newCachingPipeline(arrowStore, &failPipeline{}, "test-key", log.NewNopLogger(), testCacheStats)
	require.NoError(t, hit.Open(ctx))
	require.True(t, hit.(*cachingPipeline).hit, "expected cache hit")

	var cachedBatches []arrow.RecordBatch
	for {
		rec, err := hit.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		cachedBatches = append(cachedBatches, rec)
	}
	hit.Close()

	require.Len(t, cachedBatches, len(batches), "cached result should have same number of batches")
}

func TestArrowCacheAdapterSnappyRoundTrip(t *testing.T) {
	ctx := t.Context()
	mem := newMockCache()
	ad := &arrowCacheAdapter{
		cache:          mem,
		snappyCompress: true,
		maxSizeBytes:   ^uint64(0),
	}
	key := "snappy-key"

	t.Run("non-empty", func(t *testing.T) {
		rec, err := CSVToArrow(testFields, "x\ny")
		require.NoError(t, err)
		stored, err := ad.Set(ctx, key, []arrow.RecordBatch{rec})
		require.NoError(t, err)
		require.Greater(t, stored, 0)
		raw := mem.data[key]
		require.NotEmpty(t, raw)
		_, err = snappy.Decode(nil, raw)
		require.NoError(t, err, "stored payload should be snappy-compressed")

		got, sz, hit, err := ad.Get(ctx, key)
		require.NoError(t, err)
		require.True(t, hit)
		require.Equal(t, stored, sz)
		require.Len(t, got, 1)
		require.Equal(t, rec.NumRows(), got[0].NumRows())
	})

	t.Run("empty", func(t *testing.T) {
		emptyKey := "empty-snappy"
		stored, err := ad.Set(ctx, emptyKey, nil)
		require.NoError(t, err)
		require.Zero(t, stored, "empty entries are stored uncompressed")
		require.Empty(t, mem.data[emptyKey])
		got, sz, hit, err := ad.Get(ctx, emptyKey)
		require.NoError(t, err)
		require.True(t, hit)
		require.Zero(t, sz)
		require.Empty(t, got)
	})
}

func TestArrowCacheAdapterMaxCacheableSize(t *testing.T) {
	ctx := t.Context()

	// Build a record large enough to produce a non-trivial payload.
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = "abcdefgh"
	}
	csv := ""
	for i, r := range rows {
		if i > 0 {
			csv += "\n"
		}
		csv += r
	}
	rec, err := CSVToArrow(testFields, csv)
	require.NoError(t, err)

	t.Run("limit of zero (only cache empty)", func(t *testing.T) {
		mc := newMockCache()
		ad := &arrowCacheAdapter{
			cache:        mc,
			maxSizeBytes: 0,
			logger:       log.NewNopLogger(),
		}

		stored, err := ad.Set(ctx, "key", []arrow.RecordBatch{rec})
		require.NoError(t, err)
		require.Zero(t, stored, "should return 0 for non-empty result when maxSizeBytes==0")
		require.Equal(t, 0, mc.setCalls, "underlying Store must not be called for non-empty result")

		// Empty records (nil) should still be stored even with maxSizeBytes==0.
		storedEmpty, err := ad.Set(ctx, "empty-key", nil)
		require.NoError(t, err)
		require.Zero(t, storedEmpty)
		require.Equal(t, 1, mc.setCalls, "underlying Store must be called for empty result")
	})

	for _, tc := range []struct {
		name           string
		snappyCompress bool
	}{
		{name: "without snappy", snappyCompress: false},
		{name: "with snappy", snappyCompress: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Measure actual encoded size using an unconstrained adapter.
			probe := newMockCache()
			probeAd := &arrowCacheAdapter{
				cache:          probe,
				snappyCompress: tc.snappyCompress,
				logger:         log.NewNopLogger(),
				maxSizeBytes:   ^uint64(0),
			}
			actualSize, err := probeAd.Set(ctx, "probe-key", []arrow.RecordBatch{rec})
			require.NoError(t, err)
			require.Greater(t, actualSize, 0, "expected non-zero encoded size")

			t.Run("limit below encoded size", func(t *testing.T) {
				mc := newMockCache()
				ad := &arrowCacheAdapter{
					cache:          mc,
					snappyCompress: tc.snappyCompress,
					maxSizeBytes:   uint64(actualSize) - 1,
					logger:         log.NewNopLogger(),
				}

				stored, err := ad.Set(ctx, "key", []arrow.RecordBatch{rec})
				require.NoError(t, err)
				require.Zero(t, stored, "should return 0 when entry exceeds max size")
				require.Equal(t, 0, mc.setCalls, "underlying Store must not be called")

				_, _, hit, err := ad.Get(ctx, "key")
				require.NoError(t, err)
				require.False(t, hit, "cache should miss since nothing was stored")
			})

			t.Run("limit at encoded size", func(t *testing.T) {
				mc := newMockCache()
				ad := &arrowCacheAdapter{
					cache:          mc,
					snappyCompress: tc.snappyCompress,
					maxSizeBytes:   uint64(actualSize),
					logger:         log.NewNopLogger(),
				}

				stored, err := ad.Set(ctx, "key", []arrow.RecordBatch{rec})
				require.NoError(t, err)
				require.Equal(t, actualSize, stored, "should return full encoded size")

				got, sz, hit, err := ad.Get(ctx, "key")
				require.NoError(t, err)
				require.True(t, hit, "cache should hit")
				require.Equal(t, actualSize, sz)
				require.Len(t, got, 1)
				require.Equal(t, rec.NumRows(), got[0].NumRows())
			})
		})
	}
}

func TestCachingPipelinePassthrough(t *testing.T) {
	ctx := t.Context()

	rec1, err := CSVToArrow(testFields, "a\nb")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "c\nd")
	require.NoError(t, err)
	rec3, err := CSVToArrow(testFields, "e\nf")
	require.NoError(t, err)

	for _, tc := range []struct {
		name           string
		records        []arrow.RecordBatch
		wantBatches    int
		wantStoreCalls int
		wantKeyInCache bool
		cacheKey       string
	}{
		{
			name:           "non-empty response is not cached",
			records:        []arrow.RecordBatch{rec1, rec2, rec3},
			wantBatches:    3,
			wantStoreCalls: 0,
			wantKeyInCache: false,
			cacheKey:       "pt-key",
		},
		{
			name:           "empty response is cached",
			records:        nil,
			wantBatches:    0,
			wantStoreCalls: 1,
			wantKeyInCache: true,
			cacheKey:       "empty-key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mc := newMockCache()
			arrowStore := &arrowCacheAdapter{cache: mc, logger: log.NewNopLogger()}

			p := newCachingPipeline(arrowStore, NewBufferedPipeline(tc.records...), tc.cacheKey, log.NewNopLogger(), testCacheStats)
			require.NoError(t, p.Open(ctx))

			var batches []arrow.RecordBatch
			for {
				rec, err := p.Read(ctx)
				if errors.Is(err, EOF) {
					break
				}
				require.NoError(t, err)
				batches = append(batches, rec)
			}
			p.Close()

			require.Len(t, batches, tc.wantBatches)
			require.Equal(t, tc.wantStoreCalls, mc.setCalls)
			require.Equal(t, tc.wantKeyInCache, mc.data[cache.HashKey(tc.cacheKey)] != nil)
		})
	}
}

// failPipeline panics if Open or Read is called; used to assert inner pipeline
// is never accessed on a cache hit.
type failPipeline struct {
	closed bool
}

func (f *failPipeline) Open(_ context.Context) error {
	panic("inner pipeline must not be opened on a cache hit")
}

func (f *failPipeline) Read(_ context.Context) (arrow.RecordBatch, error) {
	panic("inner pipeline must not be read on a cache hit")
}

func (f *failPipeline) Close() { f.closed = true }

// mockCache is an in-memory cache.Cache for testing the caching pipeline.
type mockCache struct {
	data     map[string][]byte
	getCalls int
	setCalls int
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string][]byte)}
}

func (m *mockCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	m.setCalls++
	for i, key := range keys {
		m.data[key] = bufs[i]
	}
	return nil
}

func (m *mockCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	m.getCalls++
	for _, key := range keys {
		if buf, ok := m.data[key]; ok {
			found = append(found, key)
			bufs = append(bufs, buf)
		} else {
			missing = append(missing, key)
		}
	}
	return
}

func (m *mockCache) Stop() {}

func (m *mockCache) GetCacheType() stats.CacheType { return stats.TaskResultCache }
