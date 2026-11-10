package metadatacache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

var _ dataobj.WholeBlockMetadataCache = (*MemoryCache)(nil)

func TestMemoryCache_GetOrLoadMetadataRegion(t *testing.T) {
	t.Run("a miss loads and keeps the block, a later call is a hit", func(t *testing.T) {
		c := newTestMemoryCache()

		var loads int
		load := func(context.Context) ([]byte, error) {
			loads++
			return []byte("metadata-block"), nil
		}

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, []byte("metadata-block"), got)
		require.Equal(t, 1, loads)
		require.Equal(t, float64(1), testutil.ToFloat64(c.misses))
		require.Zero(t, testutil.ToFloat64(c.hits))

		got, err = c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, []byte("metadata-block"), got)
		require.Equal(t, 1, loads, "the second call is a hit")
		require.Equal(t, float64(1), testutil.ToFloat64(c.hits))
		require.Equal(t, float64(1), testutil.ToFloat64(c.misses), "a hit does not count as a miss")
	})

	t.Run("blocks of different keys are kept apart and counted", func(t *testing.T) {
		c := newTestMemoryCache()

		for key, block := range map[string]string{"a": "one", "b": "three"} {
			_, err := c.GetOrLoadMetadataRegion(t.Context(), key, func(context.Context) ([]byte, error) {
				return []byte(block), nil
			})
			require.NoError(t, err)
		}

		for key, block := range map[string]string{"a": "one", "b": "three"} {
			got, err := c.GetOrLoadMetadataRegion(t.Context(), key, func(context.Context) ([]byte, error) {
				t.Error("load must not run: the block is cached")
				return nil, nil
			})
			require.NoError(t, err)
			require.Equal(t, []byte(block), got)
		}
		require.Equal(t, 2, c.Len())
		require.Equal(t, int64(len("one")+len("three")), c.Bytes())
		require.Equal(t, float64(2), testutil.ToFloat64(c.entries))
		require.Equal(t, float64(len("one")+len("three")), testutil.ToFloat64(c.bytes))
	})

	t.Run("concurrent misses for the same key share a single load", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const callers = 8

			c := newTestMemoryCache()
			var loads atomic.Int64
			release := make(chan struct{})
			load := func(context.Context) ([]byte, error) {
				loads.Add(1)
				<-release
				return []byte("block"), nil
			}

			results := make([][]byte, callers)
			errs := make([]error, callers)
			var wg sync.WaitGroup
			for i := range callers {
				wg.Go(func() {
					results[i], errs[i] = c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
				})
			}

			synctest.Wait()
			close(release)
			wg.Wait()

			require.Equal(t, int64(1), loads.Load())
			for i := range callers {
				require.NoError(t, errs[i])
				require.Equal(t, []byte("block"), results[i])
			}
			require.Equal(t, 1, c.Len())
		})
	})

	t.Run("a failed load is not kept, so the next call loads again", func(t *testing.T) {
		c := newTestMemoryCache()
		loadErr := errors.New("load boom")

		_, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return nil, loadErr
		})
		require.ErrorIs(t, err, loadErr)
		require.Zero(t, c.Len())

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return []byte("block"), nil
		})
		require.NoError(t, err)
		require.Equal(t, []byte("block"), got)
		require.Equal(t, 1, c.Len())
	})

	t.Run("an empty block is returned but not kept", func(t *testing.T) {
		c := newTestMemoryCache()

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return nil, nil
		})
		require.NoError(t, err)
		require.Empty(t, got)
		require.Zero(t, c.Len())
	})

	t.Run("a caller that gives up does not abort the load for the others", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newTestMemoryCache()

			inLoad := make(chan struct{})
			release := make(chan struct{})
			var loads atomic.Int64
			load := func(loadCtx context.Context) ([]byte, error) {
				loads.Add(1)
				close(inLoad)
				select {
				case <-release:
					return []byte("block"), nil
				case <-loadCtx.Done():
					return nil, loadCtx.Err()
				}
			}

			ctx, cancel := context.WithCancel(t.Context())
			callerErr := make(chan error, 1)
			go func() {
				_, err := c.GetOrLoadMetadataRegion(ctx, "obj", load)
				callerErr <- err
			}()
			<-inLoad

			var wg sync.WaitGroup
			var waiterBlock []byte
			var waiterErr error
			wg.Go(func() {
				waiterBlock, waiterErr = c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
			})
			synctest.Wait()

			cancel()
			require.ErrorIs(t, <-callerErr, context.Canceled)

			close(release)
			wg.Wait()
			require.NoError(t, waiterErr)
			require.Equal(t, []byte("block"), waiterBlock)
			require.Equal(t, int64(1), loads.Load())
			require.Equal(t, 1, c.Len(), "the load finishes and keeps the block after the first caller left")
		})
	})

	t.Run("a load that outlives the timeout fails with a deadline error and keeps nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newTestMemoryCache()
			start := time.Now()

			_, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(loadCtx context.Context) ([]byte, error) {
				<-loadCtx.Done()
				return nil, loadCtx.Err()
			})

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, loadTimeout, time.Since(start))
			require.Zero(t, c.Len())
		})
	})

	t.Run("a block above the item limit is returned but not kept", func(t *testing.T) {
		c := newTestMemoryCache()
		blockLen := int(c.MaxItemBytes()) + 1

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return make([]byte, blockLen), nil
		})

		require.NoError(t, err)
		require.Len(t, got, blockLen)
		require.Zero(t, c.Len())
		require.Zero(t, c.Bytes())
	})

	t.Run("a miss on a canceled context returns its error and starts no load", func(t *testing.T) {
		c := newTestMemoryCache()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: the caller's context is done")
			return nil, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, testutil.ToFloat64(c.misses))
		require.Zero(t, c.Len())
	})

	t.Run("the load keeps the request-scoped values of the caller", func(t *testing.T) {
		c := newTestMemoryCache()

		type ctxKey struct{}
		ctx := context.WithValue(t.Context(), ctxKey{}, "trace-id")

		var got any
		_, err := c.GetOrLoadMetadataRegion(ctx, "obj", func(loadCtx context.Context) ([]byte, error) {
			got = loadCtx.Value(ctxKey{})
			return []byte("block"), nil
		})
		require.NoError(t, err)
		require.Equal(t, "trace-id", got)
	})
}

func TestMemoryCache_WithRemoteCache(t *testing.T) {
	t.Run("a miss asks the remote cache, loads once and stores the block there", func(t *testing.T) {
		backend := cache.NewMockCache()
		c := newTestMemoryCache(WithRemoteCache(newTestCache(backend, 0, nil)))

		var loads int
		load := func(context.Context) ([]byte, error) {
			loads++
			return []byte("block"), nil
		}

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, []byte("block"), got)
		require.Equal(t, 1, loads)
		require.Contains(t, backend.GetInternal(), keyPrefix+wholeBlockKeyPrefix+"obj")

		_, err = c.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)
		require.Equal(t, 1, loads, "the second call is a memory hit")
	})

	t.Run("a pod with an empty memory finds the block in the remote cache and does not load", func(t *testing.T) {
		metrics := NewMetrics(nil)
		remote := NewCache(cache.NewMockCache(), 0, KindLogs, metrics, nil)
		first := NewMemoryCache(KindLogsMemory, metrics, nil, WithRemoteCache(remote))
		second := NewMemoryCache(KindLogsMemory, metrics, nil, WithRemoteCache(remote))

		_, err := first.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return []byte("block"), nil
		})
		require.NoError(t, err)

		got, err := second.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			t.Error("load must not run: the remote cache holds the block")
			return nil, nil
		})
		require.NoError(t, err)
		require.Equal(t, []byte("block"), got)
		require.Equal(t, 1, second.Len())
		require.Equal(t, float64(1), testutil.ToFloat64(remote.hits))
		require.Equal(t, float64(1), testutil.ToFloat64(remote.misses))
		require.Equal(t, float64(2), testutil.ToFloat64(metrics.misses.WithLabelValues(KindLogsMemory)), "each memory cache missed once")
	})

	t.Run("an entry that a plain cache wrote for the same object is not reused", func(t *testing.T) {
		remote := newTestCache(cache.NewMockCache(), 0, nil)
		c := newTestMemoryCache(WithRemoteCache(remote))

		_, err := remote.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return []byte("short-region"), nil
		})
		require.NoError(t, err)

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return []byte("whole-block"), nil
		})
		require.NoError(t, err)
		require.Equal(t, []byte("whole-block"), got)
	})

	t.Run("a remote cache fetch error degrades to a load", func(t *testing.T) {
		backend := cache.NewMockCache()
		backend.SetErr(nil, errors.New("fetch boom"))
		c := newTestMemoryCache(WithRemoteCache(newTestCache(backend, 0, nil)))

		got, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return []byte("block"), nil
		})
		require.NoError(t, err)
		require.Equal(t, []byte("block"), got)
		require.Equal(t, 1, c.Len())
	})

	t.Run("a failed load is kept in neither tier", func(t *testing.T) {
		backend := cache.NewMockCache()
		c := newTestMemoryCache(WithRemoteCache(newTestCache(backend, 0, nil)))
		loadErr := errors.New("load boom")

		_, err := c.GetOrLoadMetadataRegion(t.Context(), "obj", func(context.Context) ([]byte, error) {
			return nil, loadErr
		})

		require.ErrorIs(t, err, loadErr)
		require.Zero(t, c.Len())
		require.Empty(t, backend.GetInternal())
	})
}

func newTestMemoryCache(opts ...MemoryCacheOption) *MemoryCache {
	return NewMemoryCache(KindLogsMemory, NewMetrics(nil), nil, opts...)
}
