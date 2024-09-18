package bloomshipper

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
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
	invalidDir := "not/a/valid/blockdir"
	_ = os.MkdirAll(filepath.Join(wd, invalidDir), 0o755)

	// empty block directories
	emptyDir1 := "bloom/table_1/tenant/blocks/0000000000000000-000000000000ffff/0-3600000-abcd"
	_ = os.MkdirAll(filepath.Join(wd, emptyDir1), 0o755)
	emptyDir2 := "bloom/table_1/tenant/blocks/0000000000010000-000000000001ffff/0-3600000-ef01"
	_ = os.MkdirAll(filepath.Join(wd, emptyDir2), 0o755)
	emptyDir3 := "bloom/table_1/tenant/blocks/0000000000020000-000000000002ffff/0-3600000-2345"
	_ = os.MkdirAll(filepath.Join(wd, emptyDir3), 0o755)

	// valid block directory
	validDir := "bloom/table_2/tenant/blocks/0000000000010000-000000000001ffff/0-3600000-abcd"
	_ = os.MkdirAll(filepath.Join(wd, validDir), 0o755)
	for _, fn := range []string{"bloom", "series"} {
		fp, _ = os.Create(filepath.Join(wd, validDir, fn))
		fp.Close()
	}

	cfg := config.BlocksCacheConfig{
		SoftLimit:     1 << 20,
		HardLimit:     2 << 20,
		TTL:           time.Hour,
		PurgeInterval: time.Hour,
	}
	c := NewFsBlocksCache(cfg, nil, log.NewNopLogger())

	err := LoadBlocksDirIntoCache([]string{wd, t.TempDir()}, c, logger)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.entries))

	key := validDir + ".tar.gz" // cache key must not contain directory prefix
	elem, found := c.entries[key]
	require.True(t, found)
	blockDir := elem.Value.(*Entry).Value
	require.Equal(t, filepath.Join(wd, validDir), blockDir.Path)

	// check cleaned directories
	dirs := make([]string, 0, 6)
	_ = filepath.WalkDir(wd, func(path string, dirEntry fs.DirEntry, _ error) error {
		if !dirEntry.IsDir() {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	require.Equal(t, []string{
		filepath.Join(wd),
		filepath.Join(wd, "bloom/"),
		filepath.Join(wd, "bloom/table_2/"),
		filepath.Join(wd, "bloom/table_2/tenant/"),
		filepath.Join(wd, "bloom/table_2/tenant/blocks/"),
		filepath.Join(wd, "bloom/table_2/tenant/blocks/0000000000010000-000000000001ffff"),
		filepath.Join(wd, "bloom/table_2/tenant/blocks/0000000000010000-000000000001ffff/0-3600000-abcd"),
	}, dirs)
}
