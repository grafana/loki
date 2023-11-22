package bloomshipper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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

	ctx                  context.Context
	manager              *services.Manager
	onWorkerStopCallback func()
}

func newBlockDownloader(config config.Config, blockClient BlockClient, limits Limits, logger log.Logger, reg prometheus.Registerer) (*blockDownloader, error) {
	queueMetrics := queue.NewMetrics(reg, constants.Loki, "bloom_blocks_downloader")
	//add cleanup service
	downloadingQueue := queue.NewRequestQueue(config.BlocksDownloadingQueue.MaxTasksEnqueuedPerTenant, time.Minute, queueMetrics)
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

	b := &blockDownloader{
		ctx:                  ctx,
		logger:               logger,
		workingDirectory:     config.WorkingDirectory,
		queueMetrics:         queueMetrics,
		queue:                downloadingQueue,
		blockClient:          blockClient,
		activeUsersService:   activeUsersService,
		limits:               limits,
		manager:              manager,
		onWorkerStopCallback: onWorkerStopNoopCallback,
	}

	for i := 0; i < config.BlocksDownloadingQueue.WorkersCount; i++ {
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

// noop implementation
var onWorkerStopNoopCallback = func() {}

func (d *blockDownloader) serveDownloadingTasks(workerID string) {
	logger := log.With(d.logger, "worker", workerID)
	level.Debug(logger).Log("msg", "starting worker")

	d.queue.RegisterConsumerConnection(workerID)
	defer d.queue.UnregisterConsumerConnection(workerID)
	//this callback is used only in the tests to assert that worker is stopped
	defer d.onWorkerStopCallback()

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
		blockPath := task.block.BlockPath
		//todo add cache before downloading
		level.Debug(logger).Log("msg", "start downloading the block", "block", blockPath)
		block, err := d.blockClient.GetBlock(task.ctx, task.block)
		if err != nil {
			level.Error(logger).Log("msg", "error downloading the block", "block", blockPath, "err", err)
			task.ErrCh <- fmt.Errorf("error downloading the block %s : %w", blockPath, err)
			continue
		}
		directory, err := d.extractBlock(&block, time.Now())
		if err != nil {
			level.Error(logger).Log("msg", "error extracting the block", "block", blockPath, "err", err)
			task.ErrCh <- fmt.Errorf("error extracting the block %s : %w", blockPath, err)
			continue
		}
		level.Debug(d.logger).Log("msg", "block has been downloaded and extracted", "block", task.block.BlockPath, "directory", directory)
		blockQuerier := d.createBlockQuerier(directory)
		task.ResultsCh <- blockWithQuerier{
			BlockRef:     task.block,
			BlockQuerier: blockQuerier,
		}
	}
}

func (d *blockDownloader) downloadBlocks(ctx context.Context, tenantID string, references []BlockRef) (chan blockWithQuerier, chan error) {
	d.activeUsersService.UpdateUserTimestamp(tenantID, time.Now())
	// we need to have errCh with size that can keep max count of errors to prevent the case when
	// the queue worker reported the error to this channel before the current goroutine
	// and this goroutine will go to the deadlock because it won't be able to report an error
	// because nothing reads this channel at this moment.
	errCh := make(chan error, len(references))
	blocksCh := make(chan blockWithQuerier, len(references))

	downloadingParallelism := d.limits.BloomGatewayBlocksDownloadingParallelism(tenantID)
	for _, reference := range references {
		task := NewBlockDownloadingTask(ctx, reference, blocksCh, errCh)
		level.Debug(d.logger).Log("msg", "enqueuing task to download block", "block", reference.BlockPath)
		err := d.queue.Enqueue(tenantID, nil, task, downloadingParallelism, nil)
		if err != nil {
			errCh <- fmt.Errorf("error enquing downloading task for block %s : %w", reference.BlockPath, err)
			return blocksCh, errCh
		}
	}
	return blocksCh, errCh
}

type blockWithQuerier struct {
	BlockRef
	*v1.BlockQuerier
}

// extract the files into directory and returns absolute path to this directory.
func (d *blockDownloader) extractBlock(block *Block, ts time.Time) (string, error) {
	workingDirectoryPath := filepath.Join(d.workingDirectory, block.BlockPath, strconv.FormatInt(ts.UnixMilli(), 10))
	err := os.MkdirAll(workingDirectoryPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("can not create directory to extract the block: %w", err)
	}
	archivePath, err := writeDataToTempFile(workingDirectoryPath, block)
	if err != nil {
		return "", fmt.Errorf("error writing data to temp file: %w", err)
	}
	defer func() {
		os.Remove(archivePath)
		// todo log err
	}()
	err = extractArchive(archivePath, workingDirectoryPath)
	if err != nil {
		return "", fmt.Errorf("error extracting archive: %w", err)
	}
	return workingDirectoryPath, nil
}

func (s *blockDownloader) createBlockQuerier(directory string) *v1.BlockQuerier {
	reader := v1.NewDirectoryBlockReader(directory)
	block := v1.NewBlock(reader)
	return v1.NewBlockQuerier(block)
}

func (d *blockDownloader) stop() {
	services.StopManagerAndAwaitStopped(d.ctx, d.manager)
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
