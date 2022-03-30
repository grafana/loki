package cache_test

import (
	"testing"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
)

func TestBackground(t *testing.T) {
	c := cache.NewBackground("mock", cache.BackgroundConfig{
		WriteBackGoroutines: 1,
		WriteBackBuffer:     100,
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
