package bloomshipper

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

type CloseableBlockQuerier struct {
	BlockRef
	*v1.BlockQuerier
	close func() error
}

func (c *CloseableBlockQuerier) Close() error {
	var err multierror.MultiError
	err.Add(c.BlockQuerier.Reset())
	if c.close != nil {
		err.Add(c.close())
	}
	return err.Err()
}

func (c *CloseableBlockQuerier) SeriesIter() (iter.PeekIterator[*v1.SeriesWithBlooms], error) {
	if err := c.Reset(); err != nil {
		return nil, err
	}
	return iter.NewPeekIter[*v1.SeriesWithBlooms](c.BlockQuerier.Iter()), nil
}

func LoadBlocksDirIntoCache(paths []string, c Cache, logger log.Logger) error {
	var err util.MultiError
	for _, path := range paths {
		level.Debug(logger).Log("msg", "load bloomshipper working directory into cache", "path", path)
		keys, values := loadBlockDirectories(path, logger)
		err.Add(c.PutMany(context.Background(), keys, values))
	}
	return err.Err()
}

func removeRecursively(root, path string) error {
	if path == root {
		// stop when reached root directory
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		// stop in case of error
		return err
	}

	if len(entries) == 0 {
		base := filepath.Dir(path)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return removeRecursively(root, base)
	}

	return nil
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

		// Remove empty directories recursively
		// filepath.WalkDir() does not support depth-first traversal,
		// so this is not very efficient
		err := removeRecursively(root, path)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to remove directory", "path", path, "err", err)
			return nil
		}

		// The block file extension (.tar) needs to be added so the key can be parsed.
		// This is because the extension is stripped off when the tar archive is extracted.
		ref, err := resolver.ParseBlockKey(key(path + blockExtension))
		if err != nil {
			return nil
		}

		if ok, clean := isBlockDir(path, logger); ok {
			key := cacheKey(ref)
			keys = append(keys, key)
			values = append(values, NewBlockDirectory(ref, path))
			level.Debug(logger).Log("msg", "found block directory", "path", path, "key", key)
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
func NewBlockDirectory(ref BlockRef, path string) BlockDirectory {
	bd := BlockDirectory{
		BlockRef: ref,
		Path:     path,
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
	Path string
	size int64
}

func (b BlockDirectory) Block(metrics *v1.Metrics) *v1.Block {
	return v1.NewBlock(v1.NewDirectoryBlockReader(b.Path), metrics)
}

func (b BlockDirectory) Size() int64 {
	return b.size
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
// The passed function `close` is called when the the returned querier is closed.
func (b BlockDirectory) BlockQuerier(
	alloc mempool.Allocator,
	closeFunc func() error,
	maxPageSize int,
	metrics *v1.Metrics,
) *CloseableBlockQuerier {
	return &CloseableBlockQuerier{
		BlockQuerier: v1.NewBlockQuerier(b.Block(metrics), alloc, maxPageSize),
		BlockRef:     b.BlockRef,
		close:        closeFunc,
	}
}
