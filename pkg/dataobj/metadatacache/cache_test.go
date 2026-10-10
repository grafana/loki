package metadatacache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/test"
)

// assert the adapter satisfies the dataobj interface.
var _ dataobj.MetadataCache = (*Cache)(nil)

func TestCache_MaxItemBytes(t *testing.T) {
	require.Equal(t, int64(DefaultMaxItemBytes), New(cache.NewMockCache(), 0, nil, nil).MaxItemBytes(), "a non-positive value falls back to the default")
	require.Equal(t, int64(1024), New(cache.NewMockCache(), 1024, nil, nil).MaxItemBytes())
}

func TestCache_GetOrLoadMetadataRegion(t *testing.T) {
	t.Run("a miss loads and stores, a later call is served as a hit", func(t *testing.T) {
		mc := cache.NewMockCache()
		c := New(mc, 0, nil, nil)

		var loads int
		load := func(context.Context) ([]byte, error) {
			loads++
			return []byte("metadata-blob"), nil
		}

		// Miss: loads and stores.
		got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, []byte("metadata-blob"), got)
		require.Equal(t, 1, loads)
		require.Contains(t, mc.GetInternal(), keyPrefix+"obj")
		require.Equal(t, float64(1), testutil.ToFloat64(c.misses))
		require.Zero(t, testutil.ToFloat64(c.hits))

		// Hit: served from the cache, no reload.
		got, err = c.GetOrLoadMetadataRegion(context.Background(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, []byte("metadata-blob"), got)
		require.Equal(t, 1, loads, "second call is a cache hit")
		require.Equal(t, float64(1), testutil.ToFloat64(c.hits))
		require.Equal(t, float64(1), testutil.ToFloat64(c.misses), "the hit must not also count as a miss")
	})

	t.Run("concurrent misses for the same key share a single load", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const n = 8

			c := New(cache.NewMockCache(), 0, nil, nil)

			var loads atomic.Int64
			release := make(chan struct{})
			load := func(context.Context) ([]byte, error) {
				loads.Add(1)
				<-release
				return []byte("blob"), nil
			}

			results := make([][]byte, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() {
					results[i], errs[i] = c.GetOrLoadMetadataRegion(context.Background(), "obj", load)
				})
			}

			// Wait until all goroutines have reached the singleflight gate.
			synctest.Wait()
			close(release)
			wg.Wait()

			require.Equal(t, int64(1), loads.Load(), "concurrent misses share a single load")
			for i, r := range results {
				require.NoError(t, errs[i])
				require.Equal(t, []byte("blob"), r)
			}
		})
	})

	t.Run("a fetch error falls back to a load instead of failing the call", func(t *testing.T) {
		mc := cache.NewMockCache()
		mc.SetErr(nil, errors.New("fetch boom"))
		c := New(mc, 0, nil, nil)

		var loads int
		got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			loads++
			return []byte("blob"), nil
		})
		require.NoError(t, err, "a fetch error degrades to a load, it does not fail the call")
		require.Equal(t, []byte("blob"), got)
		require.Equal(t, 1, loads)
		require.Equal(t, float64(1), testutil.ToFloat64(c.errors.WithLabelValues("fetch")))
		require.Zero(t, testutil.ToFloat64(c.misses), "a fetch error is not a fact about the key, so it must not also count as a miss")
	})

	t.Run("a caller's own canceled context takes precedence over an unrelated fetch error", func(t *testing.T) {
		mc := cache.NewMockCache()
		mc.SetErr(nil, errors.New("fetch boom"))
		c := New(mc, 0, nil, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: the caller's context was already done")
			return nil, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, testutil.ToFloat64(c.errors.WithLabelValues("fetch")), "the context wins over an unrelated backend error, so it must not count as one")
		require.Zero(t, testutil.ToFloat64(c.misses))
	})

	t.Run("a caller's own canceled context takes precedence over a cache hit", func(t *testing.T) {
		mc := cache.NewMockCache()
		require.NoError(t, mc.Store(context.Background(), []string{keyPrefix + "obj"}, [][]byte{[]byte("blob")}))
		c := New(mc, 0, nil, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: Fetch already reported a hit")
			return nil, nil
		})
		require.ErrorIs(t, err, context.Canceled, "the context wins even over a value Fetch already had ready")
		require.Zero(t, testutil.ToFloat64(c.hits), "a hit discarded for the caller's own context must not count as a hit")
	})

	t.Run("a caller's own canceled context takes precedence over a clean miss, and never triggers a load", func(t *testing.T) {
		c := New(cache.NewMockCache(), 0, nil, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: the caller's context was already done")
			return nil, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, testutil.ToFloat64(c.misses))
	})

	t.Run("a store error is logged but still returns the loaded value", func(t *testing.T) {
		mc := cache.NewMockCache()
		mc.SetErr(errors.New("store boom"), nil)
		logger := &test.CapturingLogger{}
		c := New(mc, 0, nil, logger)

		got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			return []byte("blob"), nil
		})
		require.NoError(t, err, "a store error is logged, not surfaced")
		require.Equal(t, []byte("blob"), got)
		require.Equal(t, float64(1), testutil.ToFloat64(c.errors.WithLabelValues("store")))
		require.Len(t, logger.Entries(), 1, "the store error is logged exactly once")
		require.Contains(t, logger.Entries()[0], "data object metadata cache store failed")
	})

	t.Run("a region larger than MaxItemBytes is never stored, even though the backend would accept it", func(t *testing.T) {
		mc := cache.NewMockCache()
		logger := &test.CapturingLogger{}
		c := New(mc, 3, nil, logger)

		got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			return []byte("blob"), nil // 4 bytes, over the 3-byte limit
		})
		require.NoError(t, err, "the loaded value is still returned even though it was not stored")
		require.Equal(t, []byte("blob"), got)
		require.Empty(t, mc.GetInternal(), "an oversized region must never reach the backend")
		require.Equal(t, float64(1), testutil.ToFloat64(c.errors.WithLabelValues("store")))
		require.Len(t, logger.Entries(), 1)
		require.Contains(t, logger.Entries()[0], "data object metadata cache store skipped: region exceeds configured max item size")
	})

	t.Run("the shared load's context preserves the triggering caller's request-scoped values", func(t *testing.T) {
		c := New(cache.NewMockCache(), 0, nil, nil)

		type ctxKey struct{}
		ctx := context.WithValue(context.Background(), ctxKey{}, "trace-id-123")

		var gotValue any
		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(loadCtx context.Context) ([]byte, error) {
			gotValue = loadCtx.Value(ctxKey{})
			return []byte("blob"), nil
		})
		require.NoError(t, err)
		require.Equal(t, "trace-id-123", gotValue)
	})

	t.Run("a caller's own cancellation does not abort the shared load for other callers", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			mc := cache.NewMockCache()
			c := New(mc, 0, nil, nil)

			inLoad := make(chan struct{})
			release := make(chan struct{})
			var loads atomic.Int64
			load := func(context.Context) ([]byte, error) {
				loads.Add(1)
				close(inLoad)
				<-release
				return []byte("blob"), nil
			}

			// A caller triggers the load, then has its context canceled while the load is in flight.
			ctx, cancel := context.WithCancel(context.Background())
			callerErr := make(chan error, 1)
			go func() {
				_, err := c.GetOrLoadMetadataRegion(ctx, "obj", load)
				callerErr <- err
			}()
			<-inLoad
			cancel()
			require.Error(t, <-callerErr, "the canceled caller returns its own cancellation")

			// The load was detached from the caller, so it completes and caches despite the cancellation.
			close(release)
			synctest.Wait()
			_, bufs, _, _ := mc.Fetch(context.Background(), []string{keyPrefix + "obj"})
			require.Len(t, bufs, 1, "the detached load must still complete and store")

			// A later call is served from that cached value; load is not run again.
			got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
				t.Error("load must not run: the value was cached by the detached load")
				return nil, nil
			})
			require.NoError(t, err)
			require.Equal(t, []byte("blob"), got)
			require.Equal(t, int64(1), loads.Load())
		})
	})

	t.Run("a load error surfaces to the caller", func(t *testing.T) {
		c := New(cache.NewMockCache(), 0, nil, nil)

		_, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			return nil, errors.New("load boom")
		})
		require.Error(t, err)
	})

	// A sentinel error wrapped by load must survive the singleflight round-trip unchanged: errors.Is
	// must still match it, so a caller (such as decoder.metadataViaCache) can distinguish a specific,
	// non-fatal load failure from a generic one. It must also not be misclassified as a Fetch-side
	// backend error: this is a load failure, which happens after Fetch has already missed.
	t.Run("a sentinel error wrapped by load survives the singleflight round trip", func(t *testing.T) {
		mc := cache.NewMockCache()
		c := New(mc, 0, nil, nil)
		sentinel := errors.New("cannot cache this region")

		_, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			return nil, fmt.Errorf("wrapped: %w", sentinel)
		})
		require.ErrorIs(t, err, sentinel)
		require.Zero(t, testutil.ToFloat64(c.errors.WithLabelValues("fetch")), "a load failure is not a Fetch-side backend error")
		require.Empty(t, mc.GetInternal(), "a failed load must not be stored")
	})
}

func TestNewFromConfig(t *testing.T) {
	embeddedConfig := func(prefix string) cache.Config {
		return cache.Config{
			Prefix:        prefix,
			EmbeddedCache: cache.EmbeddedCacheConfig{Enabled: true, MaxSizeMB: 1, TTL: time.Minute},
		}
	}
	memcachedConfig := func(prefix string, maxItemSize int) cache.Config {
		return cache.Config{
			Prefix:         prefix,
			MemcacheClient: cache.MemcachedClientConfig{Addresses: "localhost:11211", UpdateInterval: time.Minute, MaxItemSize: maxItemSize},
		}
	}

	t.Run("returns a working cache for an embedded backend", func(t *testing.T) {
		c, err := NewFromConfig(embeddedConfig("test.metadata-cache."), prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(c.Stop)

		var loads int
		load := func(context.Context) ([]byte, error) {
			loads++
			return []byte("metadata-blob"), nil
		}

		for range 2 {
			got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", load)
			require.NoError(t, err)
			require.Equal(t, []byte("metadata-blob"), got)
		}
		require.Equal(t, 1, loads, "the second call is served from the cache")
		require.Equal(t, float64(1), testutil.ToFloat64(c.hits))
	})

	t.Run("uses the memcached max item size as the region limit", func(t *testing.T) {
		c, err := NewFromConfig(memcachedConfig("test.metadata-cache.", 1024), prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(c.Stop)

		require.Equal(t, int64(1024), c.MaxItemBytes())
	})

	t.Run("falls back to the default limit when the memcached max item size is zero", func(t *testing.T) {
		c, err := NewFromConfig(memcachedConfig("test.metadata-cache.", 0), prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(c.Stop)

		require.Equal(t, int64(DefaultMaxItemBytes), c.MaxItemBytes())
	})

	t.Run("ignores the memcached max item size when the backend is not memcached", func(t *testing.T) {
		cfg := embeddedConfig("test.metadata-cache.")
		cfg.MemcacheClient.MaxItemSize = 1024

		c, err := NewFromConfig(cfg, prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(c.Stop)

		require.Equal(t, int64(DefaultMaxItemBytes), c.MaxItemBytes())
	})

	t.Run("returns an error when memcached and redis are both set", func(t *testing.T) {
		cfg := memcachedConfig("test.metadata-cache.", 0)
		cfg.Redis.Endpoint = "localhost:6379"

		_, err := NewFromConfig(cfg, prometheus.NewRegistry(), log.NewNopLogger())
		require.Error(t, err)
	})

	t.Run("does not panic when a sibling memcached cache shares the registry", func(t *testing.T) {
		reg := prometheus.NewRegistry()

		sibling, err := cache.New(memcachedConfig("querier.chunk-cache.", 0), reg, log.NewNopLogger(), stats.ChunkCache, "loki")
		require.NoError(t, err)
		t.Cleanup(sibling.Stop)

		require.NotPanics(t, func() {
			c, err := NewFromConfig(memcachedConfig("dataobj.metadata-cache.", 0), reg, log.NewNopLogger())
			require.NoError(t, err)
			t.Cleanup(c.Stop)
		})
	})
}
