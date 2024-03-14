package bloomshipper

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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

func TestBlockDirectory_Cleanup(t *testing.T) {
	checkInterval := 50 * time.Millisecond
	timeout := 200 * time.Millisecond

	tests := map[string]struct {
		releaseQuerier                   bool
		expectDirectoryToBeDeletedWithin time.Duration
	}{
		"expect directory to be removed once all queriers are released": {
			releaseQuerier:                   true,
			expectDirectoryToBeDeletedWithin: 2 * checkInterval,
		},
		"expect directory to be force removed after timeout": {
			releaseQuerier:                   false,
			expectDirectoryToBeDeletedWithin: 2 * timeout,
		},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			extractedBlockDirectory := t.TempDir()
			require.DirExists(t, extractedBlockDirectory)

			blockDir := BlockDirectory{
				Path:          extractedBlockDirectory,
				deleteTimeout: timeout,
				checkInterval: checkInterval,
				logger:        log.NewNopLogger(),
				refCount:      atomic.NewInt32(0),
			}
			// acquire directory
			blockDir.refCount.Inc()
			// start cleanup goroutine
			e := &cache.Entry[string, BlockDirectory]{
				Key:   blockDir.Path,
				Value: blockDir,
			}
			removeBlockDirectory(e)

			// old block dir does not exist any more
			require.NoDirExists(t, extractedBlockDirectory)

			// has been renamed
			newPath := extractedBlockDirectory + "-removed"
			require.DirExists(t, newPath)

			if tc.releaseQuerier {
				// release directory
				blockDir.refCount.Dec()
			}

			// ensure directory does not exist any more
			require.Eventually(t, func() bool {
				return !DirExists(newPath)
			}, tc.expectDirectoryToBeDeletedWithin, 10*time.Millisecond)
		})
	}
}

func Test_ClosableBlockQuerier(t *testing.T) {
	tmpDir := t.TempDir()
	// create the expected files so size initialization doesn't panic
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, v1.BloomFileName), []byte("bloom"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, v1.SeriesFileName), []byte("series"), 0o644))
	blockDir := NewBlockDirectory(BlockRef{}, tmpDir, log.NewNopLogger())

	querier := blockDir.BlockQuerier()
	require.Equal(t, int32(1), blockDir.refCount.Load())
	require.NoError(t, querier.Close())
	require.Equal(t, int32(0), blockDir.refCount.Load())
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

	c := newTypedMockCache[string, BlockDirectory]()
	err := LoadBlocksDirIntoCache(wd, c, logger)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.cache))

	key := filepath.Join(wd, fn2) + ".tar.gz"
	blockDir, found := c.cache[key]
	require.True(t, found)
	require.Equal(t, filepath.Join(wd, fn2), blockDir.Path)
}
