package bloomshipper

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

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

func LoadBlocksDirIntoCache(path string, c Cache, logger log.Logger) error {
	level.Debug(logger).Log("msg", "load bloomshipper working directory into cache", "path", path)
	keys, values := loadBlockDirectories(path, logger)
	return c.PutMany(context.Background(), keys, values)
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
			values = append(values, NewBlockDirectory(ref, path))
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
	close func() error,
	metrics *v1.Metrics,
) *CloseableBlockQuerier {
	return &CloseableBlockQuerier{
		BlockQuerier: v1.NewBlockQuerier(b.Block(metrics)),
		BlockRef:     b.BlockRef,
		close:        close,
	}
}
