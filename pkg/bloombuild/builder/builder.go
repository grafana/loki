package builder

import (
	"bytes"
	"context"
	"fmt"
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
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

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

	b.Service = services.NewBasicService(b.starting, b.running, b.stopping)
	return b, nil
}

func (b *Builder) starting(_ context.Context) error {
	b.metrics.running.Set(1)
	return nil
}

func (b *Builder) stopping(_ error) error {
	defer b.metrics.running.Set(0)

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

func (b *Builder) connectAndBuild(
	ctx context.Context,
) error {
	opts, err := b.cfg.GrpcConfig.DialOption(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create grpc dial options: %w", err)
	}

	conn, err := grpc.DialContext(ctx, b.cfg.PlannerAddress, opts...)
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

		logger := log.With(b.logger, "task", protoTask.Task.Id)

		b.metrics.taskStarted.Inc()
		start := time.Now()
		status := statusSuccess

		newMetas, err := b.processTask(c.Context(), protoTask.Task)
		if err != nil {
			status = statusFailure
			level.Error(logger).Log("msg", "failed to process task", "err", err)
		}

		b.metrics.taskCompleted.WithLabelValues(status).Inc()
		b.metrics.taskDuration.WithLabelValues(status).Observe(time.Since(start).Seconds())

		// Acknowledge task completion to planner
		if err = b.notifyTaskCompletedToPlanner(c, protoTask.Task.Id, newMetas, err); err != nil {
			return fmt.Errorf("failed to notify task completion to planner: %w", err)
		}
	}

	level.Debug(b.logger).Log("msg", "builder loop stopped")
	return nil
}

func (b *Builder) notifyTaskCompletedToPlanner(
	c protos.PlannerForBuilder_BuilderLoopClient,
	taskID string,
	metas []bloomshipper.Meta,
	err error,
) error {
	result := &protos.TaskResult{
		TaskID:       taskID,
		Error:        err,
		CreatedMetas: metas,
	}

	// We have a retry mechanism upper in the stack, but we add another one here
	// to try our best to avoid losing the task result.
	retries := backoff.New(c.Context(), b.cfg.BackoffConfig)
	for retries.Ongoing() {
		if err := c.Send(&protos.BuilderToPlanner{
			BuilderID: b.ID,
			Result:    *result.ToProtoTaskResult(),
		}); err == nil {
			break
		}

		level.Error(b.logger).Log("msg", "failed to acknowledge task completion to planner. Retrying", "err", err)
		retries.Wait()
	}

	if err := retries.Err(); err != nil {
		return fmt.Errorf("failed to acknowledge task completion to planner: %w", err)
	}

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
	protoTask *protos.ProtoTask,
) ([]bloomshipper.Meta, error) {
	task, err := protos.FromProtoTask(protoTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert proto task to task: %w", err)
	}

	client, err := b.bloomStore.Client(task.Table.ModelTime())
	if err != nil {
		level.Error(b.logger).Log("msg", "failed to get client", "err", err)
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	tenant := task.Tenant
	logger := log.With(
		b.logger,
		"tenant", tenant,
		"task", task.ID,
		"tsdb", task.TSDB.Name(),
	)
	level.Debug(logger).Log("msg", "received task")

	blockEnc, err := chunkenc.ParseEncoding(b.limits.BloomBlockEncoding(task.Tenant))
	if err != nil {
		return nil, fmt.Errorf("failed to parse block encoding: %w", err)
	}

	var (
		blockCt      int
		nGramSize    = uint64(b.limits.BloomNGramLength(tenant))
		nGramSkip    = uint64(b.limits.BloomNGramSkip(tenant))
		maxBlockSize = uint64(b.limits.BloomCompactorMaxBlockSize(tenant))
		maxBloomSize = uint64(b.limits.BloomCompactorMaxBloomSize(tenant))
		blockOpts    = v1.NewBlockOptions(blockEnc, nGramSize, nGramSkip, maxBlockSize, maxBloomSize)
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
		seriesItr, blocksIter, err := b.loadWorkForGap(ctx, task.Table, tenant, task.TSDB, gap)
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
		seriesItrWithCounter := v1.NewCounterIter[*v1.Series](seriesItr)

		gen := NewSimpleBloomGenerator(
			tenant,
			blockOpts,
			seriesItrWithCounter,
			b.chunkLoader,
			blocksIter,
			b.rwFn,
			nil, // TODO(salvacorts): Pass reporter or remove when we address tracking
			b.bloomStore.BloomMetrics(),
			logger,
		)

		level.Debug(logger).Log("msg", "generating blocks", "overlapping_blocks", len(gap.Blocks))
		newBlocks := gen.Generate(ctx)

		for newBlocks.Next() && newBlocks.Err() == nil {
			blockCt++
			blk := newBlocks.At()

			built, err := bloomshipper.BlockFrom(tenant, task.Table.Addr(), blk)
			if err != nil {
				level.Error(logger).Log("msg", "failed to build block", "err", err)
				return nil, fmt.Errorf("failed to build block: %w", err)
			}

			logger := log.With(logger, "block", built.BlockRef.String())

			if err := client.PutBlock(
				ctx,
				built,
			); err != nil {
				level.Error(logger).Log("msg", "failed to write block", "err", err)
				return nil, fmt.Errorf("failed to write block: %w", err)
			}
			b.metrics.blocksCreated.Inc()

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
	tenant string,
	id tsdb.Identifier,
	gap protos.GapWithBlocks,
) (v1.Iterator[*v1.Series], v1.CloseableResettableIterator[*v1.SeriesWithBlooms], error) {
	// load a series iterator for the gap
	seriesItr, err := b.tsdbStore.LoadTSDB(ctx, table, tenant, id, gap.Bounds)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to load tsdb")
	}

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

// TODO(owen-d): pool, evaluate if memory-only is the best choice
func (b *Builder) rwFn() (v1.BlockWriter, v1.BlockReader) {
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	return v1.NewMemoryBlockWriter(indexBuf, bloomsBuf), v1.NewByteReader(indexBuf, bloomsBuf)
}
