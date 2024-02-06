package bloomshipper

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

type ClosableBlockQuerier struct {
	*v1.BlockQuerier
	close func() error
}

func (c *ClosableBlockQuerier) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

func NewBlocksCache(config config.Config, reg prometheus.Registerer, logger log.Logger) *cache.EmbeddedCache[string, BlockDirectory] {
	return cache.NewTypedEmbeddedCache[string, BlockDirectory](
		"bloom-blocks-cache",
		config.BlocksCache.EmbeddedCacheConfig,
		reg,
		logger,
		stats.BloomBlocksCache,
		calculateBlockDirectorySize,
		func(_ string, value BlockDirectory) {
			value.removeDirectoryAsync()
		})
}

func calculateBlockDirectorySize(entry *cache.Entry[string, BlockDirectory]) uint64 {
	value := entry.Value
	bloomFileStats, _ := os.Lstat(path.Join(value.Path, v1.BloomFileName))
	seriesFileStats, _ := os.Lstat(path.Join(value.Path, v1.SeriesFileName))
	return uint64(bloomFileStats.Size() + seriesFileStats.Size())
}

func NewBlockDirectory(ref BlockRef, path string, logger log.Logger) BlockDirectory {
	return BlockDirectory{
		BlockRef:                    ref,
		Path:                        path,
		activeQueriers:              atomic.NewInt32(0),
		removeDirectoryTimeout:      time.Minute,
		logger:                      logger,
		activeQueriersCheckInterval: defaultActiveQueriersCheckInterval,
	}
}

// A BlockDirectory is a local file path that contains a bloom block.
// It maintains a counter for currently active readers.
type BlockDirectory struct {
	BlockRef
	Path                        string
	removeDirectoryTimeout      time.Duration
	activeQueriers              *atomic.Int32
	logger                      log.Logger
	activeQueriersCheckInterval time.Duration
}

func (b BlockDirectory) Block() *v1.Block {
	return v1.NewBlock(v1.NewDirectoryBlockReader(b.Path))
}

// BlockQuerier returns a new block querier from the directory.
// It increments the counter of active queriers for this directory.
// The counter is decreased when the returned querier is closed.
func (b BlockDirectory) BlockQuerier() *ClosableBlockQuerier {
	b.activeQueriers.Inc()
	return &ClosableBlockQuerier{
		BlockQuerier: v1.NewBlockQuerier(b.Block()),
		close: func() error {
			_ = b.activeQueriers.Dec()
			return nil
		},
	}
}

const defaultActiveQueriersCheckInterval = 100 * time.Millisecond

func (b *BlockDirectory) removeDirectoryAsync() {
	go func() {
		timeout := time.After(b.removeDirectoryTimeout)
		ticker := time.NewTicker(b.activeQueriersCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if b.activeQueriers.Load() == 0 {
					err := deleteFolder(b.Path)
					if err == nil {
						return
					}
					level.Error(b.logger).Log("msg", "error deleting block directory", "err", err)
				}
			case <-timeout:
				level.Warn(b.logger).Log("msg", "force deleting block folder after timeout", "timeout", b.removeDirectoryTimeout)
				err := deleteFolder(b.Path)
				if err == nil {
					return
				}
				level.Error(b.logger).Log("msg", "error force deleting block directory", "err", err)
			}
		}
	}()
}

func deleteFolder(folderPath string) error {
	err := os.RemoveAll(folderPath)
	if err != nil {
		return fmt.Errorf("error deleting bloom block directory: %w", err)
	}
	return nil
}
