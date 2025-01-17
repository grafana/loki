package cache_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/dustin/go-humanize"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

func TestBackground(t *testing.T) {
	// irrelevant in this test, set very high
	limit, err := humanize.ParseBytes("5GB")
	require.NoError(t, err)

	c := cache.NewBackground("mock", cache.BackgroundConfig{
		WriteBackGoroutines: 1,
		WriteBackBuffer:     100,
		WriteBackSizeLimit:  flagext.ByteSize(limit),
	}, cache.NewMockCache(), nil)

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}

	keys, chunks := fillCache(t, s, c)
	cache.Flush(c)

	testCacheSingle(t, c, keys, chunks)
	testCacheMultiple(t, c, keys, chunks)
	testCacheMiss(t, c)
}

func TestBackgroundSizeLimit(t *testing.T) {
	limit, err := humanize.ParseBytes("15KB")
	require.NoError(t, err)

	c := cache.NewBackground("mock", cache.BackgroundConfig{
		WriteBackGoroutines: 0,
		WriteBackBuffer:     100,
		WriteBackSizeLimit:  flagext.ByteSize(limit),
	}, cache.NewMockCache(), nil)

	ctx := context.Background()

	const firstKey = "first"
	const secondKey = "second"
	first := make([]byte, 10e3)  // 10KB
	second := make([]byte, 10e3) // 10KB
	_, _ = rand.Read(first)
	_, _ = rand.Read(second)

	// store the first 10KB
	require.NoError(t, c.Store(ctx, []string{firstKey}, [][]byte{first}))
	require.Equal(t, cache.QueueSize(c), int64(10e3))

	// second key will not be stored because it will exceed the 15KB limit
	require.NoError(t, c.Store(ctx, []string{secondKey}, [][]byte{second}))
	require.Equal(t, cache.QueueSize(c), int64(10e3))
	c.Stop()
}
