package bloomshipper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
	"k8s.io/utils/keymutex"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

type blockDownloader struct {
	logger log.Logger

	workingDirectory   string
	queueMetrics       *queue.Metrics
	queue              *queue.RequestQueue
	blockClient        BlockClient
	limits             Limits
	activeUsersService *util.ActiveUsersCleanupService

	ctx     context.Context
	manager *services.Manager
	wg      sync.WaitGroup

	blocksCache *cache.EmbeddedCache[string, *cachedBlock]
	cfg         config.Config
	keyMutex    keymutex.KeyMutex
}

type queueLimits struct {
	limits Limits
}

func (l *queueLimits) MaxConsumers(tenantID string, _ int) int {
	return l.limits.BloomGatewayBlocksDownloadingParallelism(tenantID)
}

func newBlockDownloader(config config.Config, blockClient BlockClient, limits Limits, logger log.Logger, reg prometheus.Registerer) (*blockDownloader, error) {
	queueMetrics := queue.NewMetrics(reg, constants.Loki, "bloom_blocks_downloader")
	//add cleanup service
	downloadingQueue := queue.NewRequestQueue(config.BlocksDownloadingQueue.MaxTasksEnqueuedPerTenant, time.Minute, &queueLimits{limits: limits}, queueMetrics)
	activeUsersService := util.NewActiveUsersCleanupWithDefaultValues(queueMetrics.Cleanup)

	ctx := context.Background()
	manager, err := services.NewManager(downloadingQueue, activeUsersService)
	if err != nil {
		return nil, fmt.Errorf("error creating service manager: %w", err)
	}
	err = services.StartManagerAndAwaitHealthy(ctx, manager)
	if err != nil {
		return nil, fmt.Errorf("error starting service manager: %w", err)
	}

	var blocksCache *cache.EmbeddedCache[string, *cachedBlock]
	if config.BlocksCache.EmbeddedCacheConfig.Enabled {
		blocksCache = NewBlocksCache(config, reg, logger)
	}
	b := &blockDownloader{
		ctx:                ctx,
		logger:             logger,
		workingDirectory:   config.WorkingDirectory,
		queueMetrics:       queueMetrics,
		queue:              downloadingQueue,
		blockClient:        blockClient,
		activeUsersService: activeUsersService,
		limits:             limits,
		manager:            manager,
		wg:                 sync.WaitGroup{},
		blocksCache:        blocksCache,
		cfg:                config,
		keyMutex:           keymutex.NewHashed(config.BlocksDownloadingQueue.WorkersCount),
	}

	for i := 0; i < config.BlocksDownloadingQueue.WorkersCount; i++ {
		b.wg.Add(1)
		go b.serveDownloadingTasks(fmt.Sprintf("worker-%d", i))
	}
	return b, nil
}

type BlockDownloadingTask struct {
	ctx   context.Context
	block BlockRef
	// ErrCh is a send-only channel to write an error to
	ErrCh chan<- error
	// ResultsCh is a send-only channel to return the block querier for the downloaded block
	ResultsCh chan<- blockWithQuerier
}

func NewBlockDownloadingTask(ctx context.Context, block BlockRef, resCh chan<- blockWithQuerier, errCh chan<- error) *BlockDownloadingTask {
	return &BlockDownloadingTask{
		ctx:       ctx,
		block:     block,
		ErrCh:     errCh,
		ResultsCh: resCh,
	}
}

func (d *blockDownloader) serveDownloadingTasks(workerID string) {
	// defer first, so it gets executed as last of the deferred functions
	defer d.wg.Done()

	logger := log.With(d.logger, "worker", workerID)
	level.Debug(logger).Log("msg", "starting worker")

	d.queue.RegisterConsumerConnection(workerID)
	defer d.queue.UnregisterConsumerConnection(workerID)

	idx := queue.StartIndexWithLocalQueue

	for {
		item, newIdx, err := d.queue.Dequeue(d.ctx, idx, workerID)
		if err != nil {
			if !errors.Is(err, queue.ErrStopped) && !errors.Is(err, context.Canceled) {
				level.Error(logger).Log("msg", "failed to dequeue task", "err", err)
				continue
			}
			level.Info(logger).Log("msg", "stopping worker")
			return
		}
		task, ok := item.(*BlockDownloadingTask)
		if !ok {
			level.Error(logger).Log("msg", "failed to cast to BlockDownloadingTask", "item", fmt.Sprintf("%+v", item), "type", fmt.Sprintf("%T", item))
			continue
		}

		idx = newIdx
		if d.blocksCache != nil {
			result, err := d.downloadBlockUsingCache(task, logger)
			if err != nil {
				task.ErrCh <- err
				continue
			}
			task.ResultsCh <- result
			continue
		}

		// otherwise download without cache
		directory, err := downloadBlockToDirectory(logger, task, d.workingDirectory, d.blockClient)
		if err != nil {
			task.ErrCh <- err
			continue
		}
		task.ResultsCh <- blockWithQuerier{
			BlockRef:             task.block,
			closableBlockQuerier: newBlockQuerierFromFS(directory),
		}
	}
}

func (d *blockDownloader) downloadBlockUsingCache(task *BlockDownloadingTask, logger log.Logger) (blockWithQuerier, error) {
	blockPath := task.block.BlockPath
	d.keyMutex.LockKey(blockPath)
	defer func() {
		_ = d.keyMutex.UnlockKey(blockPath)
	}()
	blockFromCache, exists := d.blocksCache.Get(task.ctx, task.block.BlockPath)
	if exists {
		return blockWithQuerier{
			BlockRef:             task.block,
			closableBlockQuerier: newBlockQuerierFromCache(blockFromCache),
		}, nil
	}

	directory, err := downloadBlockToDirectory(logger, task, d.workingDirectory, d.blockClient)
	if err != nil {
		return blockWithQuerier{}, err
	}
	blockFromCache = newCachedBlock(directory, d.cfg.BlocksCache.RemoveDirectoryGracefulPeriod, d.logger)
	err = d.blocksCache.Store(task.ctx, []string{task.block.BlockPath}, []*cachedBlock{blockFromCache})
	if err != nil {
		level.Error(logger).Log("msg", "error storing the block in the cache", "block", blockPath, "err", err)
		return blockWithQuerier{}, fmt.Errorf("error storing the block %s in the cache : %w", blockPath, err)
	}
	return blockWithQuerier{
		BlockRef:             task.block,
		closableBlockQuerier: newBlockQuerierFromCache(blockFromCache),
	}, nil
}

func downloadBlockToDirectory(logger log.Logger, task *BlockDownloadingTask, workingDirectory string, blockClient BlockClient) (string, error) {
	blockPath := task.block.BlockPath
	level.Debug(logger).Log("msg", "start downloading the block", "block", blockPath)
	block, err := blockClient.GetBlock(task.ctx, task.block)
	if err != nil {
		level.Error(logger).Log("msg", "error downloading the block", "block", blockPath, "err", err)
		return "", fmt.Errorf("error downloading the block %s : %w", blockPath, err)
	}
	directory, err := extractBlock(&block, time.Now(), workingDirectory, logger)
	if err != nil {
		level.Error(logger).Log("msg", "error extracting the block", "block", blockPath, "err", err)
		return "", fmt.Errorf("error extracting the block %s : %w", blockPath, err)
	}
	level.Debug(logger).Log("msg", "block has been downloaded and extracted", "block", task.block.BlockPath, "directory", directory)
	return directory, nil
}

func (d *blockDownloader) downloadBlocks(ctx context.Context, tenantID string, references []BlockRef) (chan blockWithQuerier, chan error) {
	d.activeUsersService.UpdateUserTimestamp(tenantID, time.Now())
	// we need to have errCh with size that can keep max count of errors to prevent the case when
	// the queue worker reported the error to this channel before the current goroutine
	// and this goroutine will go to the deadlock because it won't be able to report an error
	// because nothing reads this channel at this moment.
	errCh := make(chan error, len(references))
	blocksCh := make(chan blockWithQuerier, len(references))

	for _, reference := range references {
		task := NewBlockDownloadingTask(ctx, reference, blocksCh, errCh)
		level.Debug(d.logger).Log("msg", "enqueuing task to download block", "block", reference.BlockPath)
		err := d.queue.Enqueue(tenantID, nil, task, nil)
		if err != nil {
			errCh <- fmt.Errorf("error enquing downloading task for block %s : %w", reference.BlockPath, err)
			return blocksCh, errCh
		}
	}
	return blocksCh, errCh
}

type blockWithQuerier struct {
	BlockRef
	*closableBlockQuerier
}

// extract the files into directory and returns absolute path to this directory.
func extractBlock(block *Block, ts time.Time, workingDirectory string, logger log.Logger) (string, error) {
	workingDirectoryPath := filepath.Join(workingDirectory, block.BlockPath, strconv.FormatInt(ts.UnixNano(), 10))
	err := os.MkdirAll(workingDirectoryPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("can not create directory to extract the block: %w", err)
	}
	archivePath, err := writeDataToTempFile(workingDirectoryPath, block)
	if err != nil {
		return "", fmt.Errorf("error writing data to temp file: %w", err)
	}
	defer func() {
		err = os.Remove(archivePath)
		if err == nil {
			return
		}
		level.Error(logger).Log("msg", "error removing temp archive file", "err", err)
	}()
	err = extractArchive(archivePath, workingDirectoryPath)
	if err != nil {
		return "", fmt.Errorf("error extracting archive: %w", err)
	}
	return workingDirectoryPath, nil
}

func (d *blockDownloader) stop() {
	_ = services.StopManagerAndAwaitStopped(d.ctx, d.manager)
	d.wg.Wait()
	if d.blocksCache != nil {
		d.blocksCache.Stop()
	}
}

func writeDataToTempFile(workingDirectoryPath string, block *Block) (string, error) {
	defer block.Data.Close()
	archivePath := filepath.Join(workingDirectoryPath, block.BlockPath[strings.LastIndex(block.BlockPath, delimiter)+1:])

	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("error creating empty file to store the archiver: %w", err)
	}
	defer archiveFile.Close()
	_, err = io.Copy(archiveFile, block.Data)
	if err != nil {
		return "", fmt.Errorf("error writing data to archive file: %w", err)
	}
	return archivePath, nil
}

func extractArchive(archivePath string, workingDirectoryPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("error opening archive file %s: %w", file.Name(), err)
	}
	return v1.UnTarGz(workingDirectoryPath, file)
}

type closableBlockQuerier struct {
	*v1.BlockQuerier
	Close func() error
}

func newBlockQuerierFromCache(cached *cachedBlock) *closableBlockQuerier {
	cached.activeQueriers.Inc()
	return &closableBlockQuerier{
		BlockQuerier: createBlockQuerier(cached.blockDirectory),
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

func NewBlocksCache(config config.Config, reg prometheus.Registerer, logger log.Logger) *cache.EmbeddedCache[string, *cachedBlock] {
	return cache.NewTypedEmbeddedCache[string, *cachedBlock](
		"bloom-blocks-cache",
		config.BlocksCache.EmbeddedCacheConfig,
		reg,
		logger,
		stats.BloomBlocksCache,
		calculateBlockDirectorySize,
		func(key string, value *cachedBlock) {
			value.removeDirectoryAsync()
		})
}

func calculateBlockDirectorySize(entry *cache.Entry[string, *cachedBlock]) uint64 {
	value := entry.Value
	bloomFileStats, _ := os.Lstat(path.Join(value.blockDirectory, v1.BloomFileName))
	seriesFileStats, _ := os.Lstat(path.Join(value.blockDirectory, v1.SeriesFileName))
	return uint64(bloomFileStats.Size() + seriesFileStats.Size())
}

func newCachedBlock(blockDirectory string, removeDirectoryTimeout time.Duration, logger log.Logger) *cachedBlock {
	return &cachedBlock{
		blockDirectory:              blockDirectory,
		removeDirectoryTimeout:      removeDirectoryTimeout,
		logger:                      logger,
		activeQueriersCheckInterval: defaultActiveQueriersCheckInterval,
	}
}

type cachedBlock struct {
	blockDirectory              string
	removeDirectoryTimeout      time.Duration
	activeQueriers              atomic.Int32
	logger                      log.Logger
	activeQueriersCheckInterval time.Duration
}

const defaultActiveQueriersCheckInterval = 100 * time.Millisecond

func (b *cachedBlock) removeDirectoryAsync() {
	go func() {
		timeout := time.After(b.removeDirectoryTimeout)
		ticker := time.NewTicker(b.activeQueriersCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if b.activeQueriers.Load() == 0 {
					err := deleteFolder(b.blockDirectory)
					if err == nil {
						return
					}
					level.Error(b.logger).Log("msg", "error deleting block directory", "err", err)
				}
			case <-timeout:
				level.Warn(b.logger).Log("msg", "force deleting block folder after timeout", "timeout", b.removeDirectoryTimeout)
				err := deleteFolder(b.blockDirectory)
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
