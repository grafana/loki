package builder

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/compression"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/ring"
)

// TODO(chaudum): Make configurable via (per-tenant?) setting.
var blockCompressionAlgo = compression.EncNone

type Builder struct {
	services.Service

	ID string

	cfg     Config
	limits  Limits
	metrics *Metrics
	logger  log.Logger

	tsdbStore   common.TSDBStore
	bloomStore  bloomshipper.Store
	chunkLoader ChunkLoader

	client protos.PlannerForBuilderClient

	// used only in SSD mode where a single planner of the backend replicas needs to create tasksQueue
	// therefore is nil when planner is run in microservice mode (default)
	ringWatcher *common.RingWatcher
}

func New(
	cfg Config,
	limits Limits,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	storageMetrics storage.ClientMetrics,
	fetcherProvider stores.ChunkFetcherProvider,
	bloomStore bloomshipper.Store,
	logger log.Logger,
	r prometheus.Registerer,
	rm *ring.RingManager,
) (*Builder, error) {
	utillog.WarnExperimentalUse("Bloom Builder", logger)

	builderID := uuid.NewString()
	logger = log.With(logger, "builder_id", builderID)

	tsdbStore, err := common.NewTSDBStores(schemaCfg, storeCfg, storageMetrics, logger)
	if err != nil {
		return nil, fmt.Errorf("error creating TSDB store: %w", err)
	}

	metrics := NewMetrics(r)
	b := &Builder{
		ID:          builderID,
		cfg:         cfg,
		limits:      limits,
		metrics:     metrics,
		tsdbStore:   tsdbStore,
		bloomStore:  bloomStore,
		chunkLoader: NewStoreChunkLoader(fetcherProvider, metrics),
		logger:      logger,
	}

	if rm != nil {
		b.ringWatcher = common.NewRingWatcher(rm.RingLifecycler.GetInstanceID(), rm.Ring, time.Minute, logger)
	}

	b.Service = services.NewBasicService(b.starting, b.running, b.stopping)
	return b, nil
}

func (b *Builder) starting(ctx context.Context) error {
	if b.ringWatcher != nil {
		if err := services.StartAndAwaitRunning(ctx, b.ringWatcher); err != nil {
			return fmt.Errorf("error starting builder subservices: %w", err)
		}
	}
	b.metrics.running.Set(1)
	return nil
}

func (b *Builder) stopping(_ error) error {
	defer b.metrics.running.Set(0)

	if b.ringWatcher != nil {
		if err := services.StopAndAwaitTerminated(context.Background(), b.ringWatcher); err != nil {
			return fmt.Errorf("error stopping builder subservices: %w", err)
		}
	}

	if b.client != nil {
		// The gRPC server we use from dskit expects the orgID to be injected into the context when auth is enabled
		// We won't actually use the orgID anywhere in this service, but we need to inject it to satisfy the server.
		ctx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), "fake"))
		if err != nil {
			level.Error(b.logger).Log("msg", "failed to inject orgID into context", "err", err)
			return nil
		}

		req := &protos.NotifyBuilderShutdownRequest{
			BuilderID: b.ID,
		}
		if _, err := b.client.NotifyBuilderShutdown(ctx, req); err != nil {
			level.Error(b.logger).Log("msg", "failed to notify planner about builder shutdown", "err", err)
		}
	}

	return nil
}

func (b *Builder) running(ctx context.Context) error {
	// Retry if the connection to the planner is lost.
	retries := backoff.New(ctx, b.cfg.BackoffConfig)
	for retries.Ongoing() {
		err := b.connectAndBuild(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			break
		}

		level.Error(b.logger).Log("msg", "failed to connect and build. Retrying", "err", err)
		retries.Wait()
	}

	if err := retries.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("failed to connect and build: %w", err)
	}

	return nil
}

func (b *Builder) plannerAddress() string {
	if b.ringWatcher == nil {
		return b.cfg.PlannerAddress
	}

	addr, err := b.ringWatcher.GetLeaderAddress()
	if err != nil {
		return b.cfg.PlannerAddress
	}

	return addr
}

func (b *Builder) connectAndBuild(ctx context.Context) error {
	opts, err := b.cfg.GrpcConfig.DialOption(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create grpc dial options: %w", err)
	}

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(ctx, b.plannerAddress(), opts...)
	if err != nil {
		return fmt.Errorf("failed to dial bloom planner: %w", err)
	}

	b.client = protos.NewPlannerForBuilderClient(conn)

	// The gRPC server we use from dskit expects the orgID to be injected into the context when auth is enabled
	// We won't actually use the orgID anywhere in this service, but we need to inject it to satisfy the server.
	ctx, err = user.InjectIntoGRPCRequest(user.InjectOrgID(ctx, "fake"))
	if err != nil {
		return fmt.Errorf("failed to inject orgID into context: %w", err)
	}

	c, err := b.client.BuilderLoop(ctx)
	if err != nil {
		return fmt.Errorf("failed to start builder loop: %w", err)
	}

	// Start processing tasks from planner
	if err := b.builderLoop(c); err != nil {
		return fmt.Errorf("builder loop failed: %w", err)
	}

	return nil
}

func (b *Builder) builderLoop(c protos.PlannerForBuilder_BuilderLoopClient) error {
	// Send ready message to planner
	if err := c.Send(&protos.BuilderToPlanner{BuilderID: b.ID}); err != nil {
		return fmt.Errorf("failed to send ready message to planner: %w", err)
	}

	for b.State() == services.Running {
		// When the planner connection closes, an EOF or "planner shutting down" error is returned.
		// When the builder is shutting down, a gRPC context canceled error is returned.
		protoTask, err := c.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled {
				level.Debug(b.logger).Log("msg", "builder loop context canceled")
				return nil
			}

			return fmt.Errorf("failed to receive task from planner: %w", err)
		}

		b.metrics.processingTask.Set(1)
		b.metrics.taskStarted.Inc()
		start := time.Now()

		task, err := protos.FromProtoTask(protoTask.Task)
		if err != nil {
			task = &protos.Task{ID: protoTask.Task.Id}
			err = fmt.Errorf("failed to convert proto task to task: %w", err)
			b.logTaskCompleted(task, nil, err, start)
			if err = b.notifyTaskCompletedToPlanner(c, task, nil, err); err != nil {
				return fmt.Errorf("failed to notify task completion to planner: %w", err)
			}
			continue
		}

		newMetas, err := b.processTask(c.Context(), task)
		if err != nil {
			err = fmt.Errorf("failed to process task: %w", err)
		}

		b.logTaskCompleted(task, newMetas, err, start)
		if err = b.notifyTaskCompletedToPlanner(c, task, newMetas, err); err != nil {
			b.metrics.processingTask.Set(0)
			return fmt.Errorf("failed to notify task completion to planner: %w", err)
		}

		b.metrics.processingTask.Set(0)
	}

	level.Debug(b.logger).Log("msg", "builder loop stopped")
	return nil
}

func (b *Builder) logTaskCompleted(
	task *protos.Task,
	metas []bloomshipper.Meta,
	err error,
	start time.Time,
) {
	logger := task.GetLogger(b.logger)

	if err != nil {
		b.metrics.taskCompleted.WithLabelValues(statusFailure).Inc()
		b.metrics.taskDuration.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
		level.Debug(logger).Log(
			"msg", "task failed",
			"duration", time.Since(start).String(),
			"err", err,
		)
		return
	}

	b.metrics.taskCompleted.WithLabelValues(statusSuccess).Inc()
	b.metrics.taskDuration.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
	level.Debug(logger).Log(
		"msg", "task completed",
		"duration", time.Since(start).String(),
		"metas", len(metas),
	)
}

func (b *Builder) notifyTaskCompletedToPlanner(
	c protos.PlannerForBuilder_BuilderLoopClient,
	task *protos.Task,
	metas []bloomshipper.Meta,
	err error,
) error {
	logger := task.GetLogger(b.logger)

	result := &protos.TaskResult{
		TaskID:       task.ID,
		Error:        err,
		CreatedMetas: metas,
	}

	if err := c.Send(&protos.BuilderToPlanner{
		BuilderID: b.ID,
		Result:    *result.ToProtoTaskResult(),
	}); err != nil {
		return fmt.Errorf("failed to acknowledge task completion to planner: %w", err)
	}

	level.Debug(logger).Log("msg", "acknowledged task completion to planner")
	return nil
}

// processTask generates the blooms blocks and metas and uploads them to the object storage.
// Now that we have the gaps, we will generate a bloom block for each gap.
// We can accelerate this by using existing blocks which may already contain
// needed chunks in their blooms, for instance after a new TSDB version is generated
// but contains many of the same chunk references from the previous version.
// To do this, we'll need to take the metas we've already resolved and find blocks
// overlapping the ownership ranges we've identified as needing updates.
// With these in hand, we can download the old blocks and use them to
// accelerate bloom generation for the new blocks.
func (b *Builder) processTask(
	ctx context.Context,
	task *protos.Task,
) ([]bloomshipper.Meta, error) {
	tenant := task.Tenant
	logger := task.GetLogger(b.logger)
	level.Debug(logger).Log("msg", "task started")

	client, err := b.bloomStore.Client(task.Table.ModelTime())
	if err != nil {
		level.Error(logger).Log("msg", "failed to get client", "err", err)
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	blockEnc, err := compression.ParseEncoding(b.limits.BloomBlockEncoding(task.Tenant))
	if err != nil {
		return nil, fmt.Errorf("failed to parse block encoding: %w", err)
	}

	var (
		blockCt      int
		maxBlockSize = uint64(b.limits.BloomMaxBlockSize(tenant))
		maxBloomSize = uint64(b.limits.BloomMaxBloomSize(tenant))
		blockOpts    = v1.NewBlockOptions(blockEnc, maxBlockSize, maxBloomSize)
		created      []bloomshipper.Meta
		totalSeries  int
		bytesAdded   int
	)

	for i := range task.Gaps {
		gap := task.Gaps[i]
		logger := log.With(logger, "gap", gap.Bounds.String())

		meta := bloomshipper.Meta{
			MetaRef: bloomshipper.MetaRef{
				Ref: bloomshipper.Ref{
					TenantID:  tenant,
					TableName: task.Table.Addr(),
					Bounds:    gap.Bounds,
				},
			},
			Sources: []tsdb.SingleTenantTSDBIdentifier{task.TSDB},
		}

		// Fetch blocks that aren't up to date but are in the desired fingerprint range
		// to try and accelerate bloom creation.
		level.Debug(logger).Log("msg", "loading series and blocks for gap", "blocks", len(gap.Blocks))
		seriesItr, blocksIter, err := b.loadWorkForGap(ctx, task.Table, gap)
		if err != nil {
			level.Error(logger).Log("msg", "failed to get series and blocks", "err", err)
			return nil, fmt.Errorf("failed to get series and blocks: %w", err)
		}

		// TODO(owen-d): more elegant error handling than sync.OnceFunc
		closeBlocksIter := sync.OnceFunc(func() {
			if err := blocksIter.Close(); err != nil {
				level.Error(logger).Log("msg", "failed to close blocks iterator", "err", err)
			}
		})
		defer closeBlocksIter()

		// Blocks are built consuming the series iterator. For observability, we wrap the series iterator
		// with a counter iterator to count the number of times Next() is called on it.
		// This is used to observe the number of series that are being processed.
		seriesItrWithCounter := iter.NewCounterIter[*v1.Series](seriesItr)

		gen := NewSimpleBloomGenerator(
			tenant,
			blockOpts,
			seriesItrWithCounter,
			b.chunkLoader,
			blocksIter,
			b.writerReaderFunc,
			nil, // TODO(salvacorts): Pass reporter or remove when we address tracking
			b.bloomStore.BloomMetrics(),
			logger,
		)

		level.Debug(logger).Log("msg", "generating blocks", "overlapping_blocks", len(gap.Blocks))
		newBlocks := gen.Generate(ctx)

		for newBlocks.Next() && newBlocks.Err() == nil {
			blockCt++
			blk := newBlocks.At()

			built, err := bloomshipper.BlockFrom(blockCompressionAlgo, tenant, task.Table.Addr(), blk)
			if err != nil {
				level.Error(logger).Log("msg", "failed to build block", "err", err)
				if err = blk.Reader().Cleanup(); err != nil {
					level.Error(logger).Log("msg", "failed to cleanup block directory", "err", err)
				}
				return nil, fmt.Errorf("failed to build block: %w", err)
			}

			logger := log.With(logger, "block", built.BlockRef.String())

			if err := client.PutBlock(
				ctx,
				built,
			); err != nil {
				level.Error(logger).Log("msg", "failed to write block", "err", err)
				if err = blk.Reader().Cleanup(); err != nil {
					level.Error(logger).Log("msg", "failed to cleanup block directory", "err", err)
				}
				return nil, fmt.Errorf("failed to write block: %w", err)
			}
			b.metrics.blocksCreated.Inc()

			if err := blk.Reader().Cleanup(); err != nil {
				level.Error(logger).Log("msg", "failed to cleanup block directory", "err", err)
			}

			totalGapKeyspace := gap.Bounds.Max - gap.Bounds.Min
			progress := built.Bounds.Max - gap.Bounds.Min
			pct := float64(progress) / float64(totalGapKeyspace) * 100
			level.Debug(logger).Log("msg", "uploaded block", "progress_pct", fmt.Sprintf("%.2f", pct))

			meta.Blocks = append(meta.Blocks, built.BlockRef)
		}

		if err := newBlocks.Err(); err != nil {
			level.Error(logger).Log("msg", "failed to generate bloom", "err", err)
			return nil, fmt.Errorf("failed to generate bloom: %w", err)
		}

		closeBlocksIter()
		bytesAdded += newBlocks.Bytes()
		totalSeries += seriesItrWithCounter.Count()
		b.metrics.blocksReused.Add(float64(len(gap.Blocks)))

		// Write the new meta
		// TODO(owen-d): put total size in log, total time in metrics+log
		ref, err := bloomshipper.MetaRefFrom(tenant, task.Table.Addr(), gap.Bounds, meta.Sources, meta.Blocks)
		if err != nil {
			level.Error(logger).Log("msg", "failed to checksum meta", "err", err)
			return nil, fmt.Errorf("failed to checksum meta: %w", err)
		}
		meta.MetaRef = ref

		logger = log.With(logger, "meta", meta.MetaRef.String())

		if err := client.PutMeta(ctx, meta); err != nil {
			level.Error(logger).Log("msg", "failed to write meta", "err", err)
			return nil, fmt.Errorf("failed to write meta: %w", err)
		}

		b.metrics.metasCreated.Inc()
		level.Debug(logger).Log("msg", "uploaded meta")
		created = append(created, meta)
	}

	b.metrics.seriesPerTask.Observe(float64(totalSeries))
	b.metrics.bytesPerTask.Observe(float64(bytesAdded))
	level.Debug(logger).Log(
		"msg", "finished bloom generation",
		"blocks", blockCt,
		"series", totalSeries,
		"bytes_added", bytesAdded,
	)

	return created, nil
}

func (b *Builder) loadWorkForGap(
	ctx context.Context,
	table config.DayTable,
	gap protos.Gap,
) (iter.Iterator[*v1.Series], iter.CloseResetIterator[*v1.SeriesWithBlooms], error) {
	seriesItr := iter.NewCancelableIter[*v1.Series](ctx, iter.NewSliceIter[*v1.Series](gap.Series))

	// load a blocks iterator for the gap
	fetcher, err := b.bloomStore.Fetcher(table.ModelTime())
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get fetcher")
	}

	// NB(owen-d): we filter out nil blocks here to avoid panics in the bloom generator since the fetcher
	// input->output length and indexing in its contract
	// NB(chaudum): Do we want to fetch in strict mode and fail instead?
	f := FetchFunc[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier](func(ctx context.Context, refs []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error) {
		blks, err := fetcher.FetchBlocks(ctx, refs, bloomshipper.WithFetchAsync(false), bloomshipper.WithIgnoreNotFound(true))
		if err != nil {
			return nil, err
		}
		exists := make([]*bloomshipper.CloseableBlockQuerier, 0, len(blks))
		for _, blk := range blks {
			if blk != nil {
				exists = append(exists, blk)
			}
		}
		return exists, nil
	})
	blocksIter := newBlockLoadingIter(ctx, gap.Blocks, f, 10)

	return seriesItr, blocksIter, nil
}

func (b *Builder) writerReaderFunc() (v1.BlockWriter, v1.BlockReader) {
	dir, err := os.MkdirTemp(b.cfg.WorkingDir, "bloom-block-")
	if err != nil {
		panic(err)
	}
	return v1.NewDirectoryBlockWriter(dir), v1.NewDirectoryBlockReader(dir)
}
