package cache_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

func TestMemcached_fetchKeysBatched(t *testing.T) {
	// This test checks for three things
	// 1. `c.inputCh` is closed when `c.Stop()` is triggered
	// 2. Once `c.inputCh` is closed, no one should be writing to `c.inputCh` (thus shouldn't panic with "send to closed channel")
	// 3. Once the `ctx` is cancelled or timeout, it should stop fetching the keys.

	t.Run("inputCh is closed without panics when Stop() is triggered", func(t *testing.T) {
		client := newMockMemcache()
		m := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client, "test", nil, log.NewNopLogger(), "test")

		var (
			wg   sync.WaitGroup
			wait = make(chan struct{}) // chan to make goroutine wait till `m.Stop()` is called.
			ctx  = context.Background()
		)

		wg.Add(1)

		// This goroutine is going to do some real "work" (writing to `c.inputCh`). We then do `m.Stop()` closing `c.inputCh`. We assert there shouldn't be any panics.
		go func() {
			defer wg.Done()
			<-wait
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
		close(wait)

		wg.Wait()

	})

	t.Run("stop fetching when context cancelled", func(t *testing.T) {
		client := newMockMemcache()
		m := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client, "test", nil, log.NewNopLogger(), "test")

		var (
			wg             sync.WaitGroup
			wait           = make(chan struct{})
			ctx, ctxCancel = context.WithCancel(context.Background())
		)

		wg.Add(1)

		// This goroutine is going to do some real "work" (writing to `c.inputCh`). We then cancel passed context closing `c.inputCh`.
		// We assert there shouldn't be any panics and it stopped fetching keys.
		go func() {
			defer wg.Done()
			assert.NotPanics(t, func() {
				keys := []string{"1", "2"}
				bufs := [][]byte{[]byte("1"), []byte("2")}
				err := m.Store(ctx, keys, bufs)
				require.NoError(t, err)
				<-wait                            // wait before fetching
				_, _, _, err = m.Fetch(ctx, keys) // will try to write to `intputChan` and shouldn't panic
				require.NoError(t, err)
			})
		}()

		ctxCancel() // cancel even before single fetch is done.
		close(wait) // start the fetching
		wg.Wait()
		require.Equal(t, 0, client.getCalledCount) // client.GetMulti shouldn't have called because context is cancelled before.
		m.Stop()                                   // cancelation and Stop() should be able to work.

	})

	t.Run("stop fetching when context timeout", func(t *testing.T) {
		client := newMockMemcache()
		m := cache.NewMemcached(cache.MemcachedConfig{
			BatchSize:   10,
			Parallelism: 5,
		}, client, "test", nil, log.NewNopLogger(), "test")

		var (
			wg             sync.WaitGroup
			wait           = make(chan struct{})
			ctx, ctxCancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
		)
		wg.Add(1)

		// This goroutine is going to do some real "work" (writing to `c.inputCh`). We then wait till context timeout happens closing `c.inputCh`.
		// We assert there shouldn't be any panics and it stopped fetching keys.
		go func() {
			defer wg.Done()
			assert.NotPanics(t, func() {
				keys := []string{"1", "2"}
				bufs := [][]byte{[]byte("1"), []byte("2")}
				err := m.Store(ctx, keys, bufs)
				require.NoError(t, err)
				<-wait                            // wait before fetching
				_, _, _, err = m.Fetch(ctx, keys) // will try to write to `intputChan` and shouldn't panic
				require.NoError(t, err)
			})
		}()

		time.Sleep(105 * time.Millisecond) // wait till context timeout
		close(wait)                        // start the fetching
		wg.Wait()
		require.Equal(t, 0, client.getCalledCount) // client.GetMulti shouldn't have called because context is timedout before.
		m.Stop()                                   // cancelation and Stop() should be able to work.
		ctxCancel()                                // finally cancel context for cleanup sake

	})
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
