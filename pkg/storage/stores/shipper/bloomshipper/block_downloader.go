package bloomshipper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/utils/keymutex"

	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

type blockDownloader struct {
	logger log.Logger

	queueMetrics *queue.Metrics
	queue        *queue.RequestQueue

	limits             Limits
	activeUsersService *util.ActiveUsersCleanupService

	ctx     context.Context
	manager *services.Manager
	wg      sync.WaitGroup

	strategy downloadingStrategy
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

	strategy := createDownloadingStrategy(config, blockClient, reg, logger)
	b := &blockDownloader{
		ctx:                ctx,
		logger:             logger,
		queueMetrics:       queueMetrics,
		queue:              downloadingQueue,
		strategy:           strategy,
		activeUsersService: activeUsersService,
		limits:             limits,
		manager:            manager,
		wg:                 sync.WaitGroup{},
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
		result, err := d.strategy.downloadBlock(task, logger)
		if err != nil {
			task.ErrCh <- err
			continue
		}
		task.ResultsCh <- result
		continue
	}
}

func createDownloadingStrategy(cfg config.Config, blockClient BlockClient, reg prometheus.Registerer, logger log.Logger) downloadingStrategy {
	if cfg.BlocksCache.EmbeddedCacheConfig.Enabled {
		blocksCache := NewBlocksCache(cfg, reg, logger)
		return &cacheDownloadingStrategy{
			config:           cfg,
			workingDirectory: cfg.WorkingDirectory,
			blockClient:      blockClient,
			blocksCache:      blocksCache,
			keyMutex:         keymutex.NewHashed(cfg.BlocksDownloadingQueue.WorkersCount),
		}
	}
	return &storageDownloadingStrategy{
		workingDirectory: cfg.WorkingDirectory,
		blockClient:      blockClient,
	}
}

type downloadingStrategy interface {
	downloadBlock(task *BlockDownloadingTask, logger log.Logger) (blockWithQuerier, error)
	close()
}

type cacheDownloadingStrategy struct {
	config           config.Config
	workingDirectory string
	blockClient      BlockClient
	blocksCache      *cache.EmbeddedCache[string, CachedBlock]
	keyMutex         keymutex.KeyMutex
}

func (s *cacheDownloadingStrategy) downloadBlock(task *BlockDownloadingTask, logger log.Logger) (blockWithQuerier, error) {
	key := s.blockClient.Block(task.block).Addr()
	s.keyMutex.LockKey(key)
	defer func() {
		_ = s.keyMutex.UnlockKey(key)
	}()
	blockFromCache, exists := s.blocksCache.Get(task.ctx, key)
	if exists {
		return blockWithQuerier{
			BlockRef:             task.block,
			closableBlockQuerier: newBlockQuerierFromCache(blockFromCache),
		}, nil
	}

	directory, err := downloadBlockToDirectory(logger, task, s.workingDirectory, s.blockClient)
	if err != nil {
		return blockWithQuerier{}, err
	}
	blockFromCache = newCachedBlock(directory, s.config.BlocksCache.RemoveDirectoryGracefulPeriod, logger)
	err = s.blocksCache.Store(task.ctx, []string{key}, []CachedBlock{blockFromCache})
	if err != nil {
		level.Error(logger).Log("msg", "error storing the block in the cache", "block", key, "err", err)
		return blockWithQuerier{}, fmt.Errorf("error storing the block %s in the cache : %w", key, err)
	}
	return blockWithQuerier{
		BlockRef:             task.block,
		closableBlockQuerier: newBlockQuerierFromCache(blockFromCache),
	}, nil
}

func (s *cacheDownloadingStrategy) close() {
	s.blocksCache.Stop()
}

type storageDownloadingStrategy struct {
	workingDirectory string
	blockClient      BlockClient
}

func (s *storageDownloadingStrategy) downloadBlock(task *BlockDownloadingTask, logger log.Logger) (blockWithQuerier, error) {
	directory, err := downloadBlockToDirectory(logger, task, s.workingDirectory, s.blockClient)
	if err != nil {
		return blockWithQuerier{}, err
	}
	return blockWithQuerier{
		BlockRef:             task.block,
		closableBlockQuerier: newBlockQuerierFromFS(directory),
	}, nil
}

func (s *storageDownloadingStrategy) close() {
	// noop implementation
}

func downloadBlockToDirectory(logger log.Logger, task *BlockDownloadingTask, workingDirectory string, blockClient BlockClient) (string, error) {
	blockPath := filepath.Join(workingDirectory, blockClient.Block(task.block).LocalPath())
	level.Debug(logger).Log("msg", "start downloading the block", "block", blockPath)
	block, err := blockClient.GetBlock(task.ctx, task.block)
	if err != nil {
		level.Error(logger).Log("msg", "error downloading the block", "block", blockPath, "err", err)
		return "", fmt.Errorf("error downloading the block %s : %w", blockPath, err)
	}
	err = extractBlock(block.Data, blockPath, logger)
	if err != nil {
		level.Error(logger).Log("msg", "error extracting the block", "block", blockPath, "err", err)
		return "", fmt.Errorf("error extracting the block %s : %w", blockPath, err)
	}
	level.Debug(logger).Log("msg", "block has been downloaded and extracted", "block", blockPath)
	return blockPath, nil
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
		level.Debug(d.logger).Log("msg", "enqueuing task to download block", "block", reference)
		err := d.queue.Enqueue(tenantID, nil, task, nil)
		if err != nil {
			errCh <- fmt.Errorf("error enquing downloading task for block %s : %w", reference, err)
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
func extractBlock(data io.ReadCloser, blockDir string, logger log.Logger) error {

	err := os.MkdirAll(blockDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("can not create directory to extract the block: %w", err)
	}
	archivePath, err := writeDataToTempFile(blockDir, data)
	if err != nil {
		return fmt.Errorf("error writing data to temp file: %w", err)
	}
	defer func() {
		err = os.Remove(archivePath)
		if err != nil {
			level.Error(logger).Log("msg", "error removing temp archive file", "err", err)
		}
	}()
	err = extractArchive(archivePath, blockDir)
	if err != nil {
		return fmt.Errorf("error extracting archive: %w", err)
	}
	return nil
}

func (d *blockDownloader) stop() {
	_ = services.StopManagerAndAwaitStopped(d.ctx, d.manager)
	d.wg.Wait()
	d.strategy.close()
}
