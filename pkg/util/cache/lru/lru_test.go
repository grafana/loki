package lru

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

var (
	noopValueSupplier = func(key string) (string, error) {
		return key, nil
	}
	noopEviction = func(key, value string) {}
)

func Test_Cache(t *testing.T) {
	t.Run("cache must be created without error", func(t *testing.T) {
		cache, err := NewCache[string, string](1, noopValueSupplier, noopEviction)
		require.NoError(t, err)
		require.NotNil(t, cache)
	})
	t.Run("expected error if cache size less than 1", func(t *testing.T) {
		cache, err := NewCache[string, string](0, noopValueSupplier, noopEviction)
		require.ErrorContains(t, err, "error creating LRU cache: must provide a positive size")
		require.Nil(t, cache)
	})

	t.Run("expected error if value supplier returns error", func(t *testing.T) {
		mockValueSupplier := func(key string) (string, error) {
			return key, fmt.Errorf("error from value supplier")
		}
		cache, err := NewCache[string, string](1, mockValueSupplier, noopEviction)
		require.NoError(t, err)
		_, err = cache.Get("word")
		require.ErrorContains(t, err, "can not get the value for the key:'word' due to error: error from value supplier")
	})

	t.Run("expected eviction function to be called", func(t *testing.T) {
		mockCallsCount := 0
		mockEvictionFunc := func(key, value string) {
			mockCallsCount++
		}
		cache, err := NewCache[string, string](1, noopValueSupplier, mockEvictionFunc)
		require.NoError(t, err)
		for i := 0; i < 10; i++ {
			_, err = cache.Get(fmt.Sprintf("word-%d", i))
			require.NoError(t, err)
		}
		require.Equal(t, 9, mockCallsCount, "case size is 1, so the rest 9 keys must be evicted")
	})

	t.Run("value supplier must be called if value is not cached", func(t *testing.T) {
		mockCallsCount := atomic.NewInt32(0)
		mockValueSupplier := func(key string) (string, error) {
			mockCallsCount.Inc()
			return key, nil
		}
		cache, err := NewCache[string, string](1, mockValueSupplier, noopEviction)
		require.NoError(t, err)
		require.NotNil(t, cache)
		fromCache, err := cache.Get("word")
		require.NoError(t, err)
		require.Equal(t, "word", fromCache)
		require.Equal(t, int32(1), mockCallsCount.Load())
	})

	t.Run("value supplier must be called only once", func(t *testing.T) {
		mockCallsCount := atomic.NewInt32(0)
		mockValueSupplier := func(key string) (string, error) {
			mockCallsCount.Inc()
			return key, nil
		}
		cache, err := NewCache[string, string](1, mockValueSupplier, noopEviction)
		require.NoError(t, err)
		require.NotNil(t, cache)
		group, _ := errgroup.WithContext(context.Background())
		for i := 0; i < 100; i++ {
			group.Go(func() error {
				_, err := cache.Get("word")
				return err
			})
		}
		err = group.Wait()
		require.NoError(t, err)
		require.Equal(t, int32(1), mockCallsCount.Load())
	})

	t.Run("value supplier must be called only once even if key is a struct", func(t *testing.T) {
		mockCallsCount := atomic.NewInt32(0)
		mockValueSupplier := func(block bloomshipper.BlockRef) (string, error) {
			mockCallsCount.Inc()
			return "data for the block", nil
		}
		cache, err := NewCache[bloomshipper.BlockRef, string](1, mockValueSupplier, func(key bloomshipper.BlockRef, value string) {})
		require.NoError(t, err)
		require.NotNil(t, cache)
		group, _ := errgroup.WithContext(context.Background())
		for i := 0; i < 100; i++ {
			group.Go(func() error {
				_, err := cache.Get(bloomshipper.BlockRef{
					Ref: bloomshipper.Ref{
						TenantID:       "fake",
						TableName:      "19",
						MinFingerprint: 0xaa,
						MaxFingerprint: 0xff,
						StartTimestamp: 100,
						EndTimestamp:   300,
						Checksum:       0xab,
					},
					IndexPath: "IndexPath",
					BlockPath: "BlockPath",
				})
				return err
			})
		}
		err = group.Wait()
		require.NoError(t, err)
		require.Equal(t, int32(1), mockCallsCount.Load())
	})

	t.Run("value supplier must be called each time if value supplier returns an error", func(t *testing.T) {
		mockCallsCount := atomic.NewInt32(0)
		mockValueSupplier := func(key string) (string, error) {
			mockCallsCount.Inc()
			return "", fmt.Errorf("error")
		}
		cache, err := NewCache[string, string](1, mockValueSupplier, noopEviction)
		require.NoError(t, err)
		require.NotNil(t, cache)
		for i := 0; i < 100; i++ {
			_, err = cache.Get("word")
			require.Error(t, err)
		}
		require.Equal(t, int32(100), mockCallsCount.Load())
	})

	t.Run("other calls to the cache must not be blocked if value supplier is called for a different keys", func(t *testing.T) {
		mockCallsCount := atomic.NewInt32(0)
		mockValueSupplier := func(key string) (string, error) {
			mockCallsCount.Inc()
			time.Sleep(100 * time.Millisecond)
			return key, nil
		}
		cache, err := NewCache[string, string](10, mockValueSupplier, noopEviction)
		require.NoError(t, err)
		require.NotNil(t, cache)
		start := time.Now()
		group, _ := errgroup.WithContext(context.Background())
		for i := 0; i < 100; i++ {
			current := i
			group.Go(func() error {
				_, err := cache.Get(fmt.Sprintf("word-%d", current%10))
				return err
			})
		}
		err = group.Wait()
		require.NoError(t, err)
		require.Equal(t, int32(10), mockCallsCount.Load(), "value supplier must be called 10 time because there are only 10 unique keys")
		actualDuration := time.Since(start)
		require.LessOrEqual(t, actualDuration, 200*time.Millisecond, "value suppliers must be called in parallel, so they must complete within 100ms. added 2x just to make the test stable")
	})

}
