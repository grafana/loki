package cache_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestMemcached_fetchKeysBatched(t *testing.T) {
	// This test checks for two things
	// 1. `c.inputCh` is closed when `c.Stop()` is triggered
	// 2. Once `c.inputCh` is closed, no one should be writing to `c.inputCh` (thus shouldn't panic with "send to closed channel")

	client := newMockMemcache()
	m := cache.NewMemcached(cache.MemcachedConfig{
		BatchSize:   10,
		Parallelism: 5,
	}, client, "test", nil, log.NewNopLogger(), "test")

	var (
		wg      sync.WaitGroup
		stopped = make(chan struct{}) // chan to make goroutine wait till `m.Stop()` is called.
		ctx     = context.Background()
	)

	wg.Add(1)

	// This goroutine is going to do some real "work" (writing to `c.inputCh`). We then do `m.Stop()` closing `c.inputCh`. We assert there shouldn't be any panics.
	go func() {
		defer wg.Done()
		<-stopped
		assert.NotPanics(t, func() {
			keys := []string{"1", "2"}
			bufs := [][]byte{[]byte("1"), []byte("2")}
			err := m.Store(ctx, keys, bufs)
			require.NoError(t, err)

			_, _, _, err = m.Fetch(ctx, keys) // will try to write to `intputChan` and shouldn't panic
			require.NoError(t, err)

		})
	}()

	m.Stop()
	close(stopped)

	wg.Wait()
}

func TestMemcached(t *testing.T) {
	t.Run("unbatched", func(t *testing.T) {
		client := newMockMemcache()
		memcache := cache.NewMemcached(cache.MemcachedConfig{}, client,
			"test", nil, log.NewNopLogger(), "test")

		testMemcache(t, memcache)
	})

	t.Run("batched", func(t *testing.T) {
		client := newMockMemcache()
		memcache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client, "test", nil, log.NewNopLogger(), "test")

		testMemcache(t, memcache)
	})
}

func testMemcache(t *testing.T, memcache *cache.Memcached) {
	numKeys := 1000

	ctx := context.Background()
	keysIncMissing := make([]string, 0, numKeys)
	keys := make([]string, 0, numKeys)
	bufs := make([][]byte, 0, numKeys)

	// Insert 1000 keys skipping all multiples of 5.
	for i := 0; i < numKeys; i++ {
		keysIncMissing = append(keysIncMissing, fmt.Sprint(i))
		if i%5 == 0 {
			continue
		}

		keys = append(keys, fmt.Sprint(i))
		bufs = append(bufs, []byte(fmt.Sprint(i)))
	}
	err := memcache.Store(ctx, keys, bufs)
	require.NoError(t, err)

	found, bufs, missing, _ := memcache.Fetch(ctx, keysIncMissing)
	for i := 0; i < numKeys; i++ {
		if i%5 == 0 {
			require.Equal(t, fmt.Sprint(i), missing[0])
			missing = missing[1:]
			continue
		}

		require.Equal(t, fmt.Sprint(i), found[0])
		require.Equal(t, fmt.Sprint(i), string(bufs[0]))
		found = found[1:]
		bufs = bufs[1:]
	}
}

func TestMemcached_fetchKeysBatched_contextCancellation(t *testing.T) {
	// With Parallelism=1, batches run sequentially. Each GetMulti call takes
	// 10ms (simulated delay). A 25ms timeout allows the first two batches to
	// complete (~20ms total) but cuts off the third (~30ms).
	synctest.Test(t, func(t *testing.T) {
		keys := []string{"k1", "k2", "k3", "k4", "k5", "k6"}
		client := &delayedMockMemcache{mockMemcache: *newMockMemcache(), delay: 10 * time.Millisecond}
		for _, k := range keys {
			require.NoError(t, client.Set(&memcache.Item{Key: k, Value: []byte(k)}))
		}
		mc := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   2,
			Parallelism: 1,
		}, client, "test", nil, log.NewNopLogger(), "test")
		defer mc.Stop()

		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
		defer cancel()

		found, bufs, missed, err := mc.Fetch(ctx, keys)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Len(t, bufs, len(found))
		require.ElementsMatch(t, []string{"k1", "k2", "k3", "k4"}, found)
		require.ElementsMatch(t, []string{"k5", "k6"}, missed)
	})
}

func TestMemcachedFetchWithServerDown(t *testing.T) {
	const (
		numKeys         = 40
		downKeyInterval = 10
	)

	for _, tc := range []struct {
		name string
		cfg  cache.MemcachedConfig
	}{
		{
			name: "unbatched fetch returns the keys of the healthy servers and the server error",
			cfg:  cache.MemcachedConfig{},
		},
		{
			name: "batched fetch returns the keys of the healthy servers and the server error",
			cfg:  cache.MemcachedConfig{BatchSize: 4, Parallelism: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverDownErr := errors.New("dial tcp 10.0.0.3:11211: connect: connection refused")
			client := newMockMemcacheServerDown(serverDownErr)

			var keys, wantFound, wantMissed []string
			var wantBufs [][]byte
			for i := range numKeys {
				key := fmt.Sprintf("key-%d", i)
				keys = append(keys, key)
				require.NoError(t, client.Set(&memcache.Item{Key: key, Value: []byte(key)}))

				if i%downKeyInterval == 0 {
					client.downKeys[key] = struct{}{}
					wantMissed = append(wantMissed, key)
					continue
				}
				wantFound = append(wantFound, key)
				wantBufs = append(wantBufs, []byte(key))
			}

			mc := cache.NewMemcached(tc.cfg, client, "test", nil, log.NewNopLogger(), "test")
			defer mc.Stop()

			found, bufs, missed, err := mc.Fetch(t.Context(), keys)

			require.EqualError(t, err, serverDownErr.Error())
			require.Equal(t, wantFound, found)
			require.Equal(t, wantBufs, bufs)
			require.Equal(t, wantMissed, missed)
		})
	}
}

// mockMemcache whose calls fail 1/3rd of the time.
type mockMemcacheFailing struct {
	*mockMemcache
	calls atomic.Uint64
}

func newMockMemcacheFailing() *mockMemcacheFailing {
	return &mockMemcacheFailing{
		mockMemcache: newMockMemcache(),
	}
}

func (c *mockMemcacheFailing) GetMulti(ctx context.Context, keys []string, _ ...memcache.Option) (map[string]*memcache.Item, error) {
	calls := c.calls.Inc()
	if calls%3 == 0 {
		return nil, errors.New("fail")
	}

	return c.mockMemcache.GetMulti(ctx, keys)
}

func TestMemcacheFailure(t *testing.T) {
	t.Run("unbatched", func(t *testing.T) {
		client := newMockMemcacheFailing()
		memcache := cache.NewMemcached(cache.MemcachedConfig{}, client,
			"test", nil, log.NewNopLogger(), "test")

		testMemcacheFailing(t, memcache)
	})

	t.Run("batched", func(t *testing.T) {
		client := newMockMemcacheFailing()
		memcache := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client, "test", nil, log.NewNopLogger(), "test")

		testMemcacheFailing(t, memcache)
	})
}

func testMemcacheFailing(t *testing.T, memcache *cache.Memcached) {
	numKeys := 1000

	ctx := context.Background()
	keysIncMissing := make([]string, 0, numKeys)
	keys := make([]string, 0, numKeys)
	bufs := make([][]byte, 0, numKeys)
	// Insert 1000 keys skipping all multiples of 5.
	for i := 0; i < numKeys; i++ {
		keysIncMissing = append(keysIncMissing, fmt.Sprint(i))
		if i%5 == 0 {
			continue
		}
		keys = append(keys, fmt.Sprint(i))
		bufs = append(bufs, []byte(fmt.Sprint(i)))
	}
	err := memcache.Store(ctx, keys, bufs)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		found, bufs, missing, _ := memcache.Fetch(ctx, keysIncMissing)

		require.Equal(t, len(found), len(bufs))
		for i := range found {
			require.Equal(t, found[i], string(bufs[i]))
		}

		keysReturned := make(map[string]struct{})
		for _, key := range found {
			_, ok := keysReturned[key]
			require.False(t, ok, "duplicate key returned")

			keysReturned[key] = struct{}{}
		}
		for _, key := range missing {
			_, ok := keysReturned[key]
			require.False(t, ok, "duplicate key returned")

			keysReturned[key] = struct{}{}
		}

		for _, key := range keys {
			_, ok := keysReturned[key]
			require.True(t, ok, "key missing %s", key)
		}
	}
}

// mockMemcacheServerDown simulates a cluster with one server down. The keys in
// downKeys belong to that server. Like the real client, GetMulti returns the
// items of the other servers together with the error of the down server.
type mockMemcacheServerDown struct {
	*mockMemcache
	downKeys map[string]struct{}
	err      error
}

func newMockMemcacheServerDown(err error) *mockMemcacheServerDown {
	return &mockMemcacheServerDown{
		mockMemcache: newMockMemcache(),
		downKeys:     map[string]struct{}{},
		err:          err,
	}
}

func (m *mockMemcacheServerDown) GetMulti(ctx context.Context, keys []string, _ ...memcache.Option) (map[string]*memcache.Item, error) {
	var err error
	healthy := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, down := m.downKeys[key]; down {
			err = m.err
			continue
		}
		healthy = append(healthy, key)
	}

	items, getErr := m.mockMemcache.GetMulti(ctx, healthy)
	if getErr != nil {
		return nil, getErr
	}
	return items, err
}
