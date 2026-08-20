package metadatacache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// assert the adapter satisfies the dataobj interface.
var _ dataobj.MetadataCache = (*Cache)(nil)

// fakeCache is a map-backed cache.Cache with optional error injection.
type fakeCache struct {
	mu       sync.Mutex
	m        map[string][]byte
	fetchErr error
	storeErr error
}

func newFakeCache() *fakeCache { return &fakeCache{m: map[string][]byte{}} }

func (c *fakeCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	if c.storeErr != nil {
		return c.storeErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, k := range keys {
		c.m[k] = bufs[i]
	}
	return nil
}

func (c *fakeCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	if c.fetchErr != nil {
		return nil, nil, keys, c.fetchErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		if v, ok := c.m[k]; ok {
			found = append(found, k)
			bufs = append(bufs, v)
		} else {
			missing = append(missing, k)
		}
	}
	return found, bufs, missing, nil
}

func (c *fakeCache) Stop()                         {}
func (c *fakeCache) GetCacheType() stats.CacheType { return "test" }

func TestCache_MissThenHit(t *testing.T) {
	fc := newFakeCache()
	c := New(fc, nil, nil)

	var loads int
	load := func(context.Context) ([]byte, error) {
		loads++
		return []byte("metadata-blob"), nil
	}

	// Miss: loads and stores.
	got, err := c.GetMetadata(context.Background(), "obj", load)
	require.NoError(t, err)
	require.Equal(t, []byte("metadata-blob"), got)
	require.Equal(t, 1, loads)
	require.Contains(t, fc.m, keyPrefix+"obj")

	// Hit: served from the cache, no reload.
	got, err = c.GetMetadata(context.Background(), "obj", load)
	require.NoError(t, err)
	require.Equal(t, []byte("metadata-blob"), got)
	require.Equal(t, 1, loads, "second call is a cache hit")
}

func TestCache_Singleflight(t *testing.T) {
	fc := newFakeCache()
	c := New(fc, nil, nil)

	var loads atomic.Int64
	release := make(chan struct{})
	load := func(context.Context) ([]byte, error) {
		loads.Add(1)
		<-release // hold every in-flight load until all callers have arrived
		return []byte("blob"), nil
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([][]byte, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := c.GetMetadata(context.Background(), "obj", load)
			require.NoError(t, err)
			results[i] = b
		}()
	}
	// Give the goroutines time to all miss and coalesce on the single load, then release.
	require.Eventually(t, func() bool { return loads.Load() >= 1 }, time.Second, time.Millisecond)
	close(release)
	wg.Wait()

	require.Equal(t, int64(1), loads.Load(), "concurrent misses share a single load")
	for _, r := range results {
		require.Equal(t, []byte("blob"), r)
	}
}

func TestCache_FetchErrorFallsBackToLoad(t *testing.T) {
	fc := newFakeCache()
	fc.fetchErr = errors.New("fetch boom")
	c := New(fc, nil, nil)

	var loads int
	got, err := c.GetMetadata(context.Background(), "obj", func(context.Context) ([]byte, error) {
		loads++
		return []byte("blob"), nil
	})
	require.NoError(t, err, "a fetch error degrades to a load, it does not fail the call")
	require.Equal(t, []byte("blob"), got)
	require.Equal(t, 1, loads)
}

func TestCache_StoreErrorStillReturnsValue(t *testing.T) {
	fc := newFakeCache()
	fc.storeErr = errors.New("store boom")
	c := New(fc, nil, nil)

	got, err := c.GetMetadata(context.Background(), "obj", func(context.Context) ([]byte, error) {
		return []byte("blob"), nil
	})
	require.NoError(t, err, "a store error is logged, not surfaced")
	require.Equal(t, []byte("blob"), got)
}

func TestCache_CallerCancellationDoesNotAbortSharedLoad(t *testing.T) {
	fc := newFakeCache()
	c := New(fc, nil, nil)

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
		_, err := c.GetMetadata(ctx, "obj", load)
		callerErr <- err
	}()
	<-inLoad
	cancel()
	require.Error(t, <-callerErr, "the canceled caller returns its own cancellation")

	// The load was detached from the caller, so it completes and caches despite the cancellation.
	close(release)
	require.Eventually(t, func() bool {
		_, bufs, _, _ := fc.Fetch(context.Background(), []string{keyPrefix + "obj"})
		return len(bufs) == 1
	}, time.Second, time.Millisecond, "the detached load must still complete and store")

	// A later call is served from that cached value; load is not run again.
	got, err := c.GetMetadata(context.Background(), "obj", func(context.Context) ([]byte, error) {
		t.Error("load must not run: the value was cached by the detached load")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("blob"), got)
	require.Equal(t, int64(1), loads.Load())
}

// newMemcachedCache builds a memcached-backed cache the way cache.New does inside the modules. A memcached
// backend is required: it pulls in the shared dskit dns_lookups_total metric, keyed only by the flag prefix.
// The caller owns Stop (the underlying background loop panics if stopped twice).
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
// memcached-backed caches share one registry, and the querier and index-gateway each add a metadata cache.
//
// cache.New keys its backend metrics (including the shared dns_lookups_total) by the flag prefix, so it must
// take the plain registerer. Wrapping it with a component label would give dns_lookups_total a label name the
// sibling caches lack and panic on registration. The metadatacache counters carry no prefix, so those alone
// are component-scoped. The pre-existing sibling cache makes the mis-wiring observable here.
func TestModuleWiring_NoDuplicateRegistration(t *testing.T) {
	reg := prometheus.NewRegistry()

	// A sibling memcached cache (as the querier already has for chunks) registers dns_lookups_total{name=...}
	// with no component label. This is what a component-wrapped metadata cache would collide with.
	sibling := newMemcachedCache(t, reg, "querier.chunk-cache.")
	t.Cleanup(sibling.Stop)

	build := func(component, prefix string) {
		c := newMemcachedCache(t, reg, prefix)
		mcReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": component}, reg)
		t.Cleanup(New(c, mcReg, log.NewNopLogger()).Stop)
	}

	require.NotPanics(t, func() {
		build("querier", "querier.dataobject-metadata-cache.")
		build("index-gateway", "index-gateway.dataobject-sections.metadata-cache.")
	})
}

func TestCache_LoadErrorSurfaces(t *testing.T) {
	fc := newFakeCache()
	c := New(fc, nil, nil)

	_, err := c.GetMetadata(context.Background(), "obj", func(context.Context) ([]byte, error) {
		return nil, errors.New("load boom")
	})
	require.Error(t, err)
}
