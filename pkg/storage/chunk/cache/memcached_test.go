package cache_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

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

func (c *mockMemcacheFailing) GetMulti(keys []string, _ ...memcache.Option) (map[string]*memcache.Item, error) {
	calls := c.calls.Inc()
	if calls%3 == 0 {
		return nil, errors.New("fail")
	}

	return c.mockMemcache.GetMulti(keys)
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
