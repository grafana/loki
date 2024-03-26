package bloomshipper

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

type mockCache[K comparable, V any] struct {
	sync.Mutex
	cache map[K]V
}

func (m *mockCache[K, V]) Store(_ context.Context, keys []K, values []V) error {
	m.Lock()
	defer m.Unlock()
	for i := range keys {
		m.cache[keys[i]] = values[i]
	}
	return nil
}

func (m *mockCache[K, V]) Fetch(_ context.Context, keys []K) (found []K, values []V, missing []K, err error) {
	m.Lock()
	defer m.Unlock()
	for _, key := range keys {
		buf, ok := m.cache[key]
		if ok {
			found = append(found, key)
			values = append(values, buf)
		} else {
			missing = append(missing, key)
		}
	}
	return
}

func (m *mockCache[K, V]) Stop() {
}

func (m *mockCache[K, V]) GetCacheType() stats.CacheType {
	return "mock"
}

func newTypedMockCache[K comparable, V any]() *mockCache[K, V] {
	return &mockCache[K, V]{
		cache: make(map[K]V),
	}
}

func Test_LoadBlocksDirIntoCache(t *testing.T) {
	logger := log.NewNopLogger()
	wd := t.TempDir()

	// plain file
	fp, _ := os.Create(filepath.Join(wd, "regular-file.tar.gz"))
	fp.Close()

	// invalid directory
	_ = os.MkdirAll(filepath.Join(wd, "not/a/valid/blockdir"), 0o755)

	// empty block directory
	fn1 := "bloom/table_1/tenant/blocks/0000000000000000-000000000000ffff/0-3600000-abcd"
	_ = os.MkdirAll(filepath.Join(wd, fn1), 0o755)

	// valid block directory
	fn2 := "bloom/table_2/tenant/blocks/0000000000010000-000000000001ffff/0-3600000-abcd"
	_ = os.MkdirAll(filepath.Join(wd, fn2), 0o755)
	fp, _ = os.Create(filepath.Join(wd, fn2, "bloom"))
	fp.Close()
	fp, _ = os.Create(filepath.Join(wd, fn2, "series"))
	fp.Close()

	cfg := config.BlocksCacheConfig{
		SoftLimit:     1 << 20,
		HardLimit:     2 << 20,
		TTL:           time.Hour,
		PurgeInterval: time.Hour,
	}
	c := NewFsBlocksCache(cfg, nil, log.NewNopLogger())

	err := LoadBlocksDirIntoCache(wd, c, logger)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.entries))

	key := filepath.Join(wd, fn2) + ".tar.gz"
	elem, found := c.entries[key]
	require.True(t, found)
	blockDir := elem.Value.(*Entry).Value
	require.Equal(t, filepath.Join(wd, fn2), blockDir.Path)
}
