package metadatacache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

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
		const n = 8

		bc := &barrierCache{Cache: cache.NewMockCache(), barrierN: n, barrierCh: make(chan struct{})}
		c := New(bc, 0, nil, nil)

		var loads atomic.Int64
		release := make(chan struct{})
		load := func(context.Context) ([]byte, error) {
			loads.Add(1)
			<-release // hold every in-flight load until all callers have arrived
			return []byte("blob"), nil
		}

		var wg sync.WaitGroup
		results := make([][]byte, n)
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = c.GetOrLoadMetadataRegion(context.Background(), "obj", load)
			}()
		}
		// Wait for the barrier, then settle briefly before releasing the load: each caller still has a
		// short, unsynchronized hop from the barrier to singleflight registration to complete.
		require.Eventually(t, func() bool { return bc.arrived.Load() >= n }, time.Second, time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		close(release)
		wg.Wait()

		require.Equal(t, int64(1), loads.Load(), "concurrent misses share a single load")
		for i, r := range results {
			require.NoError(t, errs[i])
			require.Equal(t, []byte("blob"), r)
		}
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
		require.Eventually(t, func() bool {
			_, bufs, _, _ := mc.Fetch(context.Background(), []string{keyPrefix + "obj"})
			return len(bufs) == 1
		}, time.Second, time.Millisecond, "the detached load must still complete and store")

		// A later call is served from that cached value; load is not run again.
		got, err := c.GetOrLoadMetadataRegion(context.Background(), "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: the value was cached by the detached load")
			return nil, nil
		})
		require.NoError(t, err)
		require.Equal(t, []byte("blob"), got)
		require.Equal(t, int64(1), loads.Load())
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

// newMemcachedCache builds a memcached-backed cache the way cache.New does inside the modules. A
// memcached backend is required: it pulls in the shared dskit dns_lookups_total metric, keyed only by
// the flag prefix. The caller owns Stop (the underlying background loop panics if stopped twice).
func newMemcachedCache(t *testing.T, reg prometheus.Registerer, prefix string) cache.Cache {
	t.Helper()
	cfg := cache.Config{
		Prefix:         prefix,
		MemcacheClient: cache.MemcachedClientConfig{Addresses: "localhost:11211", UpdateInterval: time.Minute},
	}
	c, err := cache.New(cfg, reg, log.NewNopLogger(), stats.CacheType("dataobj-metadata"), "loki")
	require.NoError(t, err)
	return c
}

// TestModuleWiring_NoDuplicateRegistration reproduces the single-binary (-target=all) wiring: several
// memcached-backed caches share one registry, and a future wiring adds a metadata cache per component.
//
// cache.New keys its backend metrics (including the shared dns_lookups_total) by the flag prefix, so it
// must take the plain registerer. Wrapping it with a component label would give dns_lookups_total a
// label name the sibling caches lack and panic on registration. The metadatacache counters carry no
// prefix, so those alone are component-scoped. The pre-existing sibling cache makes the mis-wiring
// observable here.
func TestModuleWiring_NoDuplicateRegistration(t *testing.T) {
	reg := prometheus.NewRegistry()

	// A sibling memcached cache (as a querier already has for chunks) registers dns_lookups_total{name=...}
	// with no component label. This is what a component-wrapped metadata cache would collide with.
	sibling := newMemcachedCache(t, reg, "querier.chunk-cache.")
	t.Cleanup(sibling.Stop)

	build := func(component, prefix string) {
		c := newMemcachedCache(t, reg, prefix)
		mcReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": component}, reg)
		t.Cleanup(New(c, 0, mcReg, log.NewNopLogger()).Stop)
	}

	require.NotPanics(t, func() {
		build("querier", "querier.dataobject-metadata-cache.")
		build("index-gateway", "index-gateway.dataobject-sections.metadata-cache.")
	})
}

// barrierCache wraps a cache.Cache and makes every Fetch call wait until barrierN calls have
// arrived, then releases them all at once. A test proving singleflight coalescing needs every
// concurrent caller released from Fetch together, not merely counted: a straggler that reaches
// DoChan late enough could register only after the leader's call had already returned and been
// forgotten.
type barrierCache struct {
	cache.Cache
	barrierN  int
	barrierCh chan struct{}
	arrived   atomic.Int64
}

func (c *barrierCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	if c.arrived.Add(1) == int64(c.barrierN) {
		close(c.barrierCh)
	}
	<-c.barrierCh
	return c.Cache.Fetch(ctx, keys)
}
