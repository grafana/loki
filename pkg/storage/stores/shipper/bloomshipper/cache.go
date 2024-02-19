package bloomshipper

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

type CloseableBlockQuerier struct {
	BlockRef
	*v1.BlockQuerier
	close func() error
}

func (c *CloseableBlockQuerier) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

func (c *CloseableBlockQuerier) SeriesIter() (v1.PeekingIterator[*v1.SeriesWithBloom], error) {
	if err := c.Reset(); err != nil {
		return nil, err
	}
	return v1.NewPeekingIter[*v1.SeriesWithBloom](c.BlockQuerier), nil
}

func NewBlocksCache(cfg cache.EmbeddedCacheConfig, reg prometheus.Registerer, logger log.Logger) *cache.EmbeddedCache[string, BlockDirectory] {
	return cache.NewTypedEmbeddedCache[string, BlockDirectory](
		"bloom-blocks-cache",
		cfg,
		reg,
		logger,
		stats.BloomBlocksCache,
		directorySize,
		removeBlockDirectory,
	)
}

func LoadBlocksDirIntoCache(path string, c cache.TypedCache[string, BlockDirectory], logger log.Logger) error {
	level.Debug(logger).Log("msg", "load bloomshipper working directory into cache", "path", path)
	keys, values := loadBlockDirectories(path, logger)
	return c.Store(context.Background(), keys, values)
}

func loadBlockDirectories(root string, logger log.Logger) (keys []string, values []BlockDirectory) {
	resolver := NewPrefixedResolver(root, defaultKeyResolver{})
	_ = filepath.WalkDir(root, func(path string, dirEntry fs.DirEntry, e error) error {
		if dirEntry == nil || e != nil {
			level.Warn(logger).Log("msg", "failed to walk directory", "path", path, "dirEntry", dirEntry, "err", e)
			return nil
		}

		if !dirEntry.IsDir() {
			return nil
		}

		ref, err := resolver.ParseBlockKey(key(path))
		if err != nil {
			return nil
		}

		if ok, clean := isBlockDir(path, logger); ok {
			keys = append(keys, resolver.Block(ref).Addr())
			values = append(values, NewBlockDirectory(ref, path, logger))
			level.Debug(logger).Log("msg", "found block directory", "ref", ref, "path", path)
		} else {
			level.Warn(logger).Log("msg", "skip directory entry", "err", "not a block directory containing blooms and series", "path", path)
			_ = clean(path)
		}

		return nil
	})
	return
}

func calculateBlockDirectorySize(entry *cache.Entry[string, BlockDirectory]) uint64 {
	return uint64(entry.Value.Size())
}

// NewBlockDirectory creates a new BlockDirectory. Must exist on disk.
func NewBlockDirectory(ref BlockRef, path string, logger log.Logger) BlockDirectory {
	bd := BlockDirectory{
		BlockRef:                    ref,
		Path:                        path,
		refCount:                    atomic.NewInt32(0),
		removeDirectoryTimeout:      5 * time.Second,
		activeQueriersCheckInterval: 100 * time.Millisecond,
		logger:                      logger,
	}
	if err := bd.resolveSize(); err != nil {
		panic(err)
	}
	return bd
}

// A BlockDirectory is a local file path that contains a bloom block.
// It maintains a counter for currently active readers.
type BlockDirectory struct {
	BlockRef
	Path                        string
	refCount                    *atomic.Int32
	removeDirectoryTimeout      time.Duration
	activeQueriersCheckInterval time.Duration
	size                        int64
	logger                      log.Logger
}

// Convenience function to create a new block from a directory.
// Must not be called outside of BlockQuerier().
func (b BlockDirectory) Block() *v1.Block {
	return v1.NewBlock(v1.NewDirectoryBlockReader(b.Path))
}

func (b BlockDirectory) Size() int64 {
	return b.size
}

// Acquire increases the ref counter on the directory.
func (b BlockDirectory) Acquire() {
	_ = b.refCount.Inc()
}

// Release decreases the ref counter on the directory.
func (b BlockDirectory) Release() error {
	_ = b.refCount.Dec()
	return nil
}

func (b *BlockDirectory) resolveSize() error {
	bloomPath := filepath.Join(b.Path, v1.BloomFileName)
	bloomFileStats, err := os.Lstat(bloomPath)
	if err != nil {
		return errors.Wrapf(err, "failed to stat bloom file (%s)", bloomPath)
	}
	seriesPath := filepath.Join(b.Path, v1.SeriesFileName)
	seriesFileStats, err := os.Lstat(seriesPath)
	if err != nil {
		return errors.Wrapf(err, "failed to stat series file (%s)", seriesPath)
	}
	b.size = (bloomFileStats.Size() + seriesFileStats.Size())
	return nil
}

// BlockQuerier returns a new block querier from the directory.
// It increments the counter of active queriers for this directory.
// The counter is decreased when the returned querier is closed.
func (b BlockDirectory) BlockQuerier() *CloseableBlockQuerier {
	b.Acquire()
	return &CloseableBlockQuerier{
		BlockQuerier: v1.NewBlockQuerier(b.Block()),
		BlockRef:     b.BlockRef,
		close:        b.Release,
	}
}

func directorySize(entry *cache.Entry[string, BlockDirectory]) uint64 {
	return uint64(entry.Value.Size())
}

const defaultActiveQueriersCheckInterval = 100 * time.Millisecond

// removeBlockDirectory is called by the cache when an item is evicted
// The cache key and the cache value are passed to this function.
// The function needs to be synchronous, because otherwise we could get a cache
// race condition where the item is already evicted from the cache, but the
// underlying directory isn't.
func removeBlockDirectory(entry *cache.Entry[string, BlockDirectory]) {
	b := entry.Value

	timeout := time.After(b.removeDirectoryTimeout)
	ticker := time.NewTicker(b.activeQueriersCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if b.refCount.Load() == 0 {
				if err := os.RemoveAll(b.Path); err != nil {
					level.Error(b.logger).Log("msg", "error deleting block directory", "err", err)
				}
				return
			}
		case <-timeout:
			level.Warn(b.logger).Log("msg", "force deleting block folder after timeout", "timeout", b.removeDirectoryTimeout)
			if err := os.RemoveAll(b.Path); err != nil {
				level.Error(b.logger).Log("msg", "error force deleting block directory", "err", err)
			}
			return
		}
	}
}
