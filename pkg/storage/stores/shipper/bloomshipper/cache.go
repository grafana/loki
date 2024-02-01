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

type closableBlockQuerier struct {
	*v1.BlockQuerier
	Close func() error
}

func newBlockQuerierFromCache(cached CachedBlock) *closableBlockQuerier {
	cached.activeQueriers.Inc()
	return &closableBlockQuerier{
		BlockQuerier: createBlockQuerier(cached.Directory),
		Close: func() error {
			cached.activeQueriers.Dec()
			return nil
		},
	}
}

func newBlockQuerierFromFS(blockDirectory string) *closableBlockQuerier {
	return &closableBlockQuerier{
		BlockQuerier: createBlockQuerier(blockDirectory),
		Close: func() error {
			return deleteFolder(blockDirectory)
		},
	}
}

func createBlockQuerier(directory string) *v1.BlockQuerier {
	reader := v1.NewDirectoryBlockReader(directory)
	block := v1.NewBlock(reader)
	return v1.NewBlockQuerier(block)
}

func NewBlocksCache(config config.Config, reg prometheus.Registerer, logger log.Logger) *cache.EmbeddedCache[string, CachedBlock] {
	return cache.NewTypedEmbeddedCache[string, CachedBlock](
		"bloom-blocks-cache",
		config.BlocksCache.EmbeddedCacheConfig,
		reg,
		logger,
		stats.BloomBlocksCache,
		calculateBlockDirectorySize,
		func(_ string, value CachedBlock) {
			value.removeDirectoryAsync()
		})
}

func calculateBlockDirectorySize(entry *cache.Entry[string, CachedBlock]) uint64 {
	value := entry.Value
	bloomFileStats, _ := os.Lstat(path.Join(value.Directory, v1.BloomFileName))
	seriesFileStats, _ := os.Lstat(path.Join(value.Directory, v1.SeriesFileName))
	return uint64(bloomFileStats.Size() + seriesFileStats.Size())
}

func newCachedBlock(blockDirectory string, removeDirectoryTimeout time.Duration, logger log.Logger) CachedBlock {
	return CachedBlock{
		Directory:                   blockDirectory,
		activeQueriers:              atomic.NewInt32(0),
		removeDirectoryTimeout:      removeDirectoryTimeout,
		logger:                      logger,
		activeQueriersCheckInterval: defaultActiveQueriersCheckInterval,
	}
}

type CachedBlock struct {
	Directory                   string
	removeDirectoryTimeout      time.Duration
	activeQueriers              *atomic.Int32
	logger                      log.Logger
	activeQueriersCheckInterval time.Duration
}

const defaultActiveQueriersCheckInterval = 100 * time.Millisecond

func (b *CachedBlock) removeDirectoryAsync() {
	go func() {
		timeout := time.After(b.removeDirectoryTimeout)
		ticker := time.NewTicker(b.activeQueriersCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if b.activeQueriers.Load() == 0 {
					err := deleteFolder(b.Directory)
					if err == nil {
						return
					}
					level.Error(b.logger).Log("msg", "error deleting block directory", "err", err)
				}
			case <-timeout:
				level.Warn(b.logger).Log("msg", "force deleting block folder after timeout", "timeout", b.removeDirectoryTimeout)
				err := deleteFolder(b.Directory)
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
