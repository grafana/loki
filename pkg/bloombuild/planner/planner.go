package planner

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/util"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/ring"
)

var (
	errPlannerIsNotRunning = errors.New("planner is not running")
	errPlannerIsNotLeader  = errors.New("planner is not leader")
)

type Planner struct {
	services.Service
	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	retentionManager   *RetentionManager

	cfg       Config
	limits    Limits
	schemaCfg config.SchemaConfig

	tsdbStore  common.TSDBStore
	bloomStore bloomshipper.StoreBase

	tasksQueue  *queue.RequestQueue
	activeUsers *util.ActiveUsersCleanupService

	pendingTasks sync.Map

	metrics *Metrics
	logger  log.Logger

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
	bloomStore bloomshipper.StoreBase,
	logger log.Logger,
	r prometheus.Registerer,
	rm *ring.RingManager,
) (*Planner, error) {
	utillog.WarnExperimentalUse("Bloom Planner", logger)

	tsdbStore, err := common.NewTSDBStores(schemaCfg, storeCfg, storageMetrics, logger)
	if err != nil {
		return nil, fmt.Errorf("error creating TSDB store: %w", err)
	}

	// Queue to manage tasks
	queueMetrics := NewQueueMetrics(r)
	tasksQueue := queue.NewRequestQueue(cfg.MaxQueuedTasksPerTenant, 0, NewQueueLimits(limits), queueMetrics)

	// Clean metrics for inactive users: do not have added tasks to the queue in the last 1 hour
	activeUsers := util.NewActiveUsersCleanupService(5*time.Minute, 1*time.Hour, func(user string) {
		queueMetrics.Cleanup(user)
	})

	p := &Planner{
		cfg:         cfg,
		limits:      limits,
		schemaCfg:   schemaCfg,
		tsdbStore:   tsdbStore,
		bloomStore:  bloomStore,
		tasksQueue:  tasksQueue,
		activeUsers: activeUsers,
		metrics:     NewMetrics(r, tasksQueue.GetConnectedConsumersMetric),
		logger:      logger,
	}

	p.retentionManager = NewRetentionManager(
		p.cfg.RetentionConfig,
		p.limits,
		p.bloomStore,
		p.metrics,
		p.logger,
	)

	svcs := []services.Service{p.tasksQueue, p.activeUsers}

	if rm != nil {
		p.ringWatcher = common.NewRingWatcher(rm.RingLifecycler.GetInstanceID(), rm.Ring, time.Minute, logger)
		svcs = append(svcs, p.ringWatcher)
	}

	p.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return nil, fmt.Errorf("error creating subservices manager: %w", err)
	}
	p.subservicesWatcher = services.NewFailureWatcher()
	p.subservicesWatcher.WatchManager(p.subservices)

	p.Service = services.NewBasicService(p.starting, p.running, p.stopping)
	return p, nil
}

func (p *Planner) isLeader() bool {
	if p.ringWatcher == nil {
		// when the planner runs as standalone service in microserivce mode, then there is no ringWatcher
		// therefore we can safely assume that the planner is a singleton
		return true
	}
	return p.ringWatcher.IsLeader()
}

func (p *Planner) starting(ctx context.Context) (err error) {
	if err := services.StartManagerAndAwaitHealthy(ctx, p.subservices); err != nil {
		return fmt.Errorf("error starting planner subservices: %w", err)
	}

	p.metrics.running.Set(1)
	return nil
}

func (p *Planner) stopping(_ error) error {
	defer p.metrics.running.Set(0)

	// This will also stop the requests queue, which stop accepting new requests and errors out any pending requests.
	if err := services.StopManagerAndAwaitStopped(context.Background(), p.subservices); err != nil {
		return fmt.Errorf("error stopping planner subservices: %w", err)
	}

	return nil
}

func (p *Planner) running(ctx context.Context) error {
	go p.trackInflightRequests(ctx)

	// run once at beginning, but delay by 1m to allow ring consolidation when running in SSD mode
	initialPlanningTimer := time.NewTimer(time.Minute)
	defer initialPlanningTimer.Stop()

	planningTicker := time.NewTicker(p.cfg.PlanningInterval)
	defer planningTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); !errors.Is(err, context.Canceled) {
				level.Error(p.logger).Log("msg", "planner context done with error", "err", err)
				return err
			}

			level.Debug(p.logger).Log("msg", "planner context done")
			return nil

		case <-initialPlanningTimer.C:
			level.Info(p.logger).Log("msg", "starting initial bloom build iteration")
			if err := p.runOne(ctx); err != nil {
				level.Error(p.logger).Log("msg", "initial bloom build iteration failed", "err", err)
			}

		case <-planningTicker.C:
			level.Info(p.logger).Log("msg", "starting bloom build iteration")
			if err := p.runOne(ctx); err != nil {
				level.Error(p.logger).Log("msg", "bloom build iteration failed", "err", err)
			}
		}
	}
}

func (p *Planner) trackInflightRequests(ctx context.Context) {
	inflightTasksTicker := time.NewTicker(250 * time.Millisecond)
	defer inflightTasksTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// We just return. Error handling and logging is done in the main loop (running method).
			return

		case <-inflightTasksTicker.C:
			inflight := p.totalPendingTasks()
			p.metrics.inflightRequests.Observe(float64(inflight))
		}
	}
}

type tenantTableTaskResults struct {
	tasksToWait   int
	originalMetas []bloomshipper.Meta
	resultsCh     chan *protos.TaskResult
}

type tenantTable struct {
	table  config.DayTable
	tenant string
}

func (p *Planner) runOne(ctx context.Context) error {
	if !p.isLeader() {
		return errPlannerIsNotLeader
	}

	var (
		wg     sync.WaitGroup
		start  = time.Now()
		status = statusFailure
	)
	defer func() {
		p.metrics.buildCompleted.WithLabelValues(status).Inc()
		p.metrics.buildTime.WithLabelValues(status).Observe(time.Since(start).Seconds())

		if status == statusSuccess {
			p.metrics.buildLastSuccess.SetToCurrentTime()
		}
	}()

	p.metrics.buildStarted.Inc()
	level.Info(p.logger).Log("msg", "running bloom build iteration")

	// Launch retention (will return instantly if retention is disabled)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := p.retentionManager.Apply(ctx); err != nil {
			level.Error(p.logger).Log("msg", "failed apply retention", "err", err)
		}
	}()

	tables := p.tables(time.Now())
	level.Debug(p.logger).Log("msg", "loaded tables", "tables", tables.TotalDays())

	work, err := p.loadTenantWork(ctx, tables)
	if err != nil {
		return fmt.Errorf("error loading work: %w", err)
	}

	// For deletion, we need to aggregate the results for each table and tenant tuple
	// We cannot delete the returned tombstoned metas as soon as a task finishes since
	// other tasks may still be using the now tombstoned metas
	tasksResultForTenantTable := make(map[tenantTable]tenantTableTaskResults)
	var totalTasks int

	for table, tenants := range work {
		for tenant, ownershipRanges := range tenants {
			logger := log.With(p.logger, "tenant", tenant, "table", table.Addr())
			tt := tenantTable{
				tenant: tenant,
				table:  table,
			}

			tasks, existingMetas, err := p.computeTasks(ctx, table, tenant, ownershipRanges)
			if err != nil {
				level.Error(logger).Log("msg", "error computing tasks", "err", err)
				continue
			}
			level.Debug(logger).Log("msg", "computed tasks", "tasks", len(tasks), "existingMetas", len(existingMetas))

			var tenantTableEnqueuedTasks int
			resultsCh := make(chan *protos.TaskResult, len(tasks))

			now := time.Now()
			for _, task := range tasks {
				queueTask := NewQueueTask(ctx, now, task, resultsCh)
				if err := p.enqueueTask(queueTask); err != nil {
					level.Error(logger).Log("msg", "error enqueuing task", "err", err)
					continue
				}

				totalTasks++
				tenantTableEnqueuedTasks++
			}

			p.metrics.tenantTasksPlanned.WithLabelValues(tt.tenant).Add(float64(tenantTableEnqueuedTasks))
			tasksResultForTenantTable[tt] = tenantTableTaskResults{
				tasksToWait:   tenantTableEnqueuedTasks,
				originalMetas: existingMetas,
				resultsCh:     resultsCh,
			}

			level.Debug(logger).Log("msg", "enqueued tasks", "tasks", tenantTableEnqueuedTasks)
		}
	}

	p.metrics.planningTime.Observe(time.Since(start).Seconds())
	level.Debug(p.logger).Log(
		"msg", "planning completed",
		"tenantTables", len(tasksResultForTenantTable),
		"tasks", totalTasks,
		"time", time.Since(start).Seconds(),
	)

	// Create a goroutine to process the results for each table tenant tuple
	// TODO(salvacorts): This may end up creating too many goroutines.
	//                   Create a pool of workers to process table-tenant tuples.
	var tasksSucceed atomic.Int64
	for tt, results := range tasksResultForTenantTable {
		if results.tasksToWait == 0 {
			// No tasks enqueued for this tenant-table tuple, skip processing
			continue
		}

		wg.Add(1)
		go func(table config.DayTable, tenant string, results tenantTableTaskResults) {
			defer wg.Done()

			logger := log.With(p.logger, "table", table.Addr(), "tenant", tenant)

			nSucceed, err := p.processTenantTaskResults(
				ctx, table, tenant,
				results.originalMetas, results.tasksToWait, results.resultsCh,
			)
			if err != nil {
				level.Error(logger).Log("msg", "failed to process tenant task results", "err", err)
			}

			if nSucceed != results.tasksToWait {
				level.Error(logger).Log(
					"msg", "not all tasks succeeded for tenant table",
					"tasks", results.tasksToWait,
					"tasksSucceed", nSucceed,
					"tasksFailed", results.tasksToWait-nSucceed,
				)
			}
			tasksSucceed.Add(int64(nSucceed))
		}(tt.table, tt.tenant, results)
	}

	level.Debug(p.logger).Log(
		"msg", "waiting for all tasks to be completed",
		"tenantTables", len(tasksResultForTenantTable),
		"tasks", totalTasks,
	)
	wg.Wait()

	status = statusSuccess
	level.Info(p.logger).Log(
		"msg", "bloom build iteration completed",
		"tasks", totalTasks,
		"tasksSucceed", tasksSucceed.Load(),
		"duration", time.Since(start).Seconds(),
	)
	return nil
}

// computeTasks computes the tasks for a given table and tenant and ownership range.
// It returns the tasks to be executed and the metas that are existing relevant for the ownership range.
func (p *Planner) computeTasks(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	ownershipRanges []v1.FingerprintBounds,
) ([]*protos.Task, []bloomshipper.Meta, error) {
	var tasks []*protos.Task
	logger := log.With(p.logger, "table", table.Addr(), "tenant", tenant)

	// Fetch source metas to be used in both build and cleanup of out-of-date metas+blooms
	metas, err := p.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.NewInterval(table.Bounds()),
			Keyspace: v1.NewBounds(0, math.MaxUint64),
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get metas: %w", err)
	}

	// In case the planner restarted before deleting outdated metas in the previous iteration,
	// we delete them during the planning phase to avoid reprocessing them.
	metas, err = p.deleteOutdatedMetasAndBlocks(ctx, table, tenant, nil, metas, phasePlanning)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to delete outdated metas during planning: %w", err)
	}

	// Resolve TSDBs
	tsdbs, err := p.tsdbStore.ResolveTSDBs(ctx, table, tenant)
	if err != nil {
		level.Error(logger).Log("msg", "failed to resolve tsdbs", "err", err)
		return nil, nil, fmt.Errorf("failed to resolve tsdbs: %w", err)
	}

	if len(tsdbs) == 0 {
		return nil, metas, nil
	}

	openTSDBs, err := openAllTSDBs(ctx, table, tenant, p.tsdbStore, tsdbs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open all tsdbs: %w", err)
	}
	defer func() {
		for idx, reader := range openTSDBs {
			if err := reader.Close(); err != nil {
				level.Error(logger).Log("msg", "failed to close index", "err", err, "tsdb", idx.Name())
			}
		}
	}()

	for _, ownershipRange := range ownershipRanges {
		logger := log.With(logger, "ownership", ownershipRange.String())

		// Filter only the metas that overlap in the ownership range
		metasInBounds := bloomshipper.FilterMetasOverlappingBounds(metas, ownershipRange)

		// Find gaps in the TSDBs for this tenant/table
		gaps, err := p.findOutdatedGaps(ctx, tenant, openTSDBs, ownershipRange, metasInBounds, logger)
		if err != nil {
			level.Error(logger).Log("msg", "failed to find outdated gaps", "err", err)
			continue
		}

		for _, gap := range gaps {
			tasks = append(tasks, protos.NewTask(table, tenant, ownershipRange, gap.tsdb, gap.gaps))
		}
	}

	return tasks, metas, nil
}

func (p *Planner) processTenantTaskResults(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	originalMetas []bloomshipper.Meta,
	totalTasks int,
	resultsCh <-chan *protos.TaskResult,
) (int, error) {
	logger := log.With(p.logger, "table", table.Addr(), "tenant", tenant)
	level.Debug(logger).Log("msg", "waiting for all tasks to be completed", "tasks", totalTasks)

	var tasksSucceed int
	newMetas := make([]bloomshipper.Meta, 0, totalTasks)
	for i := 0; i < totalTasks; i++ {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				level.Error(logger).Log("msg", "planner context done with error", "err", err)
				return tasksSucceed, err
			}

			// No error or context canceled, just return
			level.Debug(logger).Log("msg", "context done while waiting for task results")
			return tasksSucceed, nil
		case result := <-resultsCh:
			if result == nil {
				p.metrics.tenantTasksCompleted.WithLabelValues(tenant, statusFailure).Inc()
				level.Error(logger).Log("msg", "received nil task result")
				continue
			}
			if result.Error != nil {
				p.metrics.tenantTasksCompleted.WithLabelValues(tenant, statusFailure).Inc()
				level.Error(logger).Log(
					"msg", "task failed",
					"err", result.Error,
					"task", result.TaskID,
				)
				continue
			}

			p.metrics.tenantTasksCompleted.WithLabelValues(tenant, statusSuccess).Inc()
			newMetas = append(newMetas, result.CreatedMetas...)
			tasksSucceed++
		}
	}

	level.Debug(logger).Log(
		"msg", "all tasks completed for tenant table",
		"tasks", totalTasks,
		"tasksSucceed", tasksSucceed,
		"originalMetas", len(originalMetas),
		"newMetas", len(newMetas),
	)

	if len(newMetas) == 0 {
		// No new metas were created, nothing to delete
		// Note: this would only happen if all tasks failed
		return tasksSucceed, nil
	}

	if _, err := p.deleteOutdatedMetasAndBlocks(ctx, table, tenant, newMetas, originalMetas, phaseBuilding); err != nil {
		return 0, fmt.Errorf("failed to delete outdated metas: %w", err)
	}

	return tasksSucceed, nil
}

func openAllTSDBs(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	store common.TSDBStore,
	tsdbs []tsdb.SingleTenantTSDBIdentifier,
) (map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries, error) {
	openTSDBs := make(map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries, len(tsdbs))
	for _, idx := range tsdbs {
		tsdb, err := store.LoadTSDB(ctx, table, tenant, idx)
		if err != nil {
			return nil, fmt.Errorf("failed to load tsdb: %w", err)
		}

		openTSDBs[idx] = tsdb
	}

	return openTSDBs, nil
}

// deleteOutdatedMetasAndBlocks filters out the outdated metas from the `metas` argument and deletes them from the store.
// It returns the up-to-date metas from the `metas` argument.
func (p *Planner) deleteOutdatedMetasAndBlocks(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	newMetas []bloomshipper.Meta,
	originalMetas []bloomshipper.Meta,
	phase string,
) ([]bloomshipper.Meta, error) {
	logger := log.With(p.logger, "table", table.Addr(), "tenant", tenant, "phase", phase)

	combined := append(originalMetas, newMetas...)
	upToDate, outdated := outdatedMetas(combined)
	if len(outdated) == 0 {
		level.Debug(logger).Log(
			"msg", "no outdated metas found",
			"upToDate", len(upToDate),
		)
		return upToDate, nil
	}

	level.Debug(logger).Log(
		"msg", "found outdated metas",
		"outdated", len(outdated),
		"upToDate", len(upToDate),
	)

	client, err := p.bloomStore.Client(table.ModelTime())
	if err != nil {
		level.Error(logger).Log("msg", "failed to get client", "err", err)
		return nil, errors.Wrap(err, "failed to get client")
	}

	var (
		deletedMetas  int
		deletedBlocks int
	)
	defer func() {
		p.metrics.metasDeleted.WithLabelValues(phase).Add(float64(deletedMetas))
		p.metrics.blocksDeleted.WithLabelValues(phase).Add(float64(deletedBlocks))
	}()

	for _, meta := range outdated {
		for _, block := range meta.Blocks {
			logger := log.With(logger, "block", block.String())

			// Prevent deleting blocks that are reused in new metas
			if isBlockInMetas(block, upToDate) {
				level.Debug(logger).Log("msg", "block is still in use in new meta, skipping delete")
				continue
			}

			if err := client.DeleteBlocks(ctx, []bloomshipper.BlockRef{block}); err != nil {
				if client.IsObjectNotFoundErr(err) {
					level.Debug(logger).Log("msg", "block not found while attempting delete, continuing")
				} else {
					level.Error(logger).Log("msg", "failed to delete block", "err", err)
					return nil, errors.Wrap(err, "failed to delete block")
				}
			}

			deletedBlocks++
			level.Debug(logger).Log("msg", "removed outdated block")
		}

		err = client.DeleteMetas(ctx, []bloomshipper.MetaRef{meta.MetaRef})
		if err != nil {
			if client.IsObjectNotFoundErr(err) {
				level.Debug(logger).Log("msg", "meta not found while attempting delete, continuing", "meta", meta.MetaRef.String())
			} else {
				level.Error(logger).Log("msg", "failed to delete meta", "err", err, "meta", meta.MetaRef.String())
				return nil, errors.Wrap(err, "failed to delete meta")
			}
		}
		deletedMetas++
		level.Debug(logger).Log("msg", "removed outdated meta", "meta", meta.MetaRef.String())
	}

	level.Debug(logger).Log(
		"msg", "deleted outdated metas and blocks",
		"metas", deletedMetas,
		"blocks", deletedBlocks,
	)

	return upToDate, nil
}

func isBlockInMetas(block bloomshipper.BlockRef, metas []bloomshipper.Meta) bool {
	// Blocks are sorted within a meta, so we can find it with binary search
	for _, meta := range metas {
		// Search for the first block whose bound is >= than the target block min bound.
		i := sort.Search(len(meta.Blocks), func(i int) bool {
			return meta.Blocks[i].Cmp(uint64(block.Bounds.Max)) <= v1.Overlap
		})

		if i < len(meta.Blocks) && meta.Blocks[i] == block {
			return true
		}
	}

	return false
}

func (p *Planner) tables(ts time.Time) *dayRangeIterator {
	// adjust the minimum by one to make it inclusive, which is more intuitive
	// for a configuration variable
	adjustedMin := p.cfg.MinTableOffset - 1
	minCompactionDelta := time.Duration(adjustedMin) * config.ObjectStorageIndexRequiredPeriod
	maxCompactionDelta := time.Duration(p.cfg.MaxTableOffset) * config.ObjectStorageIndexRequiredPeriod

	from := ts.Add(-maxCompactionDelta).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)
	through := ts.Add(-minCompactionDelta).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)

	fromDay := config.NewDayTime(model.TimeFromUnixNano(from))
	throughDay := config.NewDayTime(model.TimeFromUnixNano(through))
	level.Debug(p.logger).Log("msg", "loaded tables for compaction", "from", fromDay, "through", throughDay)
	return newDayRangeIterator(fromDay, throughDay, p.schemaCfg)
}

type work map[config.DayTable]map[string][]v1.FingerprintBounds

// loadTenantWork loads the work for each tenant and table tuple.
// work is the list of fingerprint ranges that need to be indexed in bloom filters.
func (p *Planner) loadTenantWork(
	ctx context.Context,
	tables *dayRangeIterator,
) (work, error) {
	tenantTableWork := make(map[config.DayTable]map[string][]v1.FingerprintBounds, tables.TotalDays())

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {
		table := tables.At()
		level.Debug(p.logger).Log("msg", "loading work for table", "table", table)

		tenants, err := p.tenants(ctx, table)
		if err != nil {
			return nil, fmt.Errorf("error loading tenants: %w", err)
		}
		level.Debug(p.logger).Log("msg", "loaded tenants", "table", table, "tenants", tenants.Remaining())

		// If this is the first this we see this table, initialize the map
		if tenantTableWork[table] == nil {
			tenantTableWork[table] = make(map[string][]v1.FingerprintBounds, tenants.Remaining())
		}

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			p.metrics.tenantsDiscovered.Inc()
			tenant := tenants.At()

			if !p.limits.BloomCreationEnabled(tenant) {
				level.Debug(p.logger).Log("msg", "bloom creation disabled for tenant", "tenant", tenant)
				continue
			}

			splitFactor := p.limits.BloomSplitSeriesKeyspaceBy(tenant)
			bounds := SplitFingerprintKeyspaceByFactor(splitFactor)

			tenantTableWork[table][tenant] = bounds

			// Reset progress tracking metrics for this tenant
			// NOTE(salvacorts): We will reset them multiple times for the same tenant, for each table, but it's not a big deal.
			//                   Alternatively, we can use a Counter instead of a Gauge, but I think a Gauge is easier to reason about.
			p.metrics.tenantTasksPlanned.WithLabelValues(tenant).Set(0)
			p.metrics.tenantTasksCompleted.WithLabelValues(tenant, statusSuccess).Set(0)
			p.metrics.tenantTasksCompleted.WithLabelValues(tenant, statusFailure).Set(0)

			level.Debug(p.logger).Log("msg", "loading work for tenant", "table", table, "tenant", tenant, "splitFactor", splitFactor)
		}
		if err := tenants.Err(); err != nil {
			level.Error(p.logger).Log("msg", "error iterating tenants", "err", err)
			return nil, fmt.Errorf("error iterating tenants: %w", err)
		}

	}
	if err := tables.Err(); err != nil {
		level.Error(p.logger).Log("msg", "error iterating tables", "err", err)
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return tenantTableWork, ctx.Err()
}

func (p *Planner) tenants(ctx context.Context, table config.DayTable) (*iter.SliceIter[string], error) {
	tenants, err := p.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, fmt.Errorf("error loading tenants for table (%s): %w", table, err)
	}

	return iter.NewSliceIter(tenants), nil
}

// blockPlan is a plan for all the work needed to build a meta.json
// It includes:
//   - the tsdb (source of truth) which contains all the series+chunks
//     we need to ensure are indexed in bloom blocks
//   - a list of gaps that are out of date and need to be checked+built
//   - within each gap, a list of block refs which overlap the gap are included
//     so we can use them to accelerate bloom generation. They likely contain many
//     of the same chunks we need to ensure are indexed, just from previous tsdb iterations.
//     This is a performance optimization to avoid expensive re-reindexing
type blockPlan struct {
	tsdb tsdb.SingleTenantTSDBIdentifier
	gaps []protos.Gap
}

func (p *Planner) findOutdatedGaps(
	ctx context.Context,
	tenant string,
	tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
	ownershipRange v1.FingerprintBounds,
	metas []bloomshipper.Meta,
	logger log.Logger,
) ([]blockPlan, error) {
	// Determine which TSDBs have gaps in the ownership range and need to
	// be processed.
	tsdbsWithGaps, err := gapsBetweenTSDBsAndMetas(ownershipRange, tsdbs, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to find gaps", "err", err)
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}

	if len(tsdbsWithGaps) == 0 {
		level.Debug(logger).Log("msg", "blooms exist for all tsdbs")
		return nil, nil
	}

	work, err := blockPlansForGaps(ctx, tenant, tsdbsWithGaps, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create plan", "err", err)
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	return work, nil
}

// Used to signal the gaps that need to be populated for a tsdb
type tsdbGaps struct {
	tsdbIdentifier tsdb.SingleTenantTSDBIdentifier
	tsdb           common.ClosableForSeries
	gaps           []v1.FingerprintBounds
}

// gapsBetweenTSDBsAndMetas returns if the metas are up-to-date with the TSDBs. This is determined by asserting
// that for each TSDB, there are metas covering the entire ownership range which were generated from that specific TSDB.
func gapsBetweenTSDBsAndMetas(
	ownershipRange v1.FingerprintBounds,
	tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
	metas []bloomshipper.Meta,
) (res []tsdbGaps, err error) {
	for db, tsdb := range tsdbs {
		id := db.Name()

		relevantMetas := make([]v1.FingerprintBounds, 0, len(metas))
		for _, meta := range metas {
			for _, s := range meta.Sources {
				if s.Name() == id {
					relevantMetas = append(relevantMetas, meta.Bounds)
				}
			}
		}

		gaps, err := FindGapsInFingerprintBounds(ownershipRange, relevantMetas)
		if err != nil {
			return nil, err
		}

		if len(gaps) > 0 {
			res = append(res, tsdbGaps{
				tsdbIdentifier: db,
				tsdb:           tsdb,
				gaps:           gaps,
			})
		}
	}

	return res, err
}

// blockPlansForGaps groups tsdb gaps we wish to fill with overlapping but out of date blocks.
// This allows us to expedite bloom generation by using existing blocks to fill in the gaps
// since many will contain the same chunks.
func blockPlansForGaps(
	ctx context.Context,
	tenant string,
	tsdbs []tsdbGaps,
	metas []bloomshipper.Meta,
) ([]blockPlan, error) {
	plans := make([]blockPlan, 0, len(tsdbs))

	for _, idx := range tsdbs {
		plan := blockPlan{
			tsdb: idx.tsdbIdentifier,
			gaps: make([]protos.Gap, 0, len(idx.gaps)),
		}

		for _, gap := range idx.gaps {
			planGap := protos.Gap{
				Bounds: gap,
			}

			seriesItr, err := common.NewTSDBSeriesIter(ctx, tenant, idx.tsdb, gap)
			if err != nil {
				return nil, fmt.Errorf("failed to load series from TSDB for gap (%s): %w", gap.String(), err)
			}
			planGap.Series, err = iter.Collect(seriesItr)
			if err != nil {
				return nil, fmt.Errorf("failed to collect series: %w", err)
			}

			for _, meta := range metas {
				if meta.Bounds.Intersection(gap) == nil {
					// this meta doesn't overlap the gap, skip
					continue
				}

				for _, block := range meta.Blocks {
					if block.Bounds.Intersection(gap) == nil {
						// this block doesn't overlap the gap, skip
						continue
					}
					// this block overlaps the gap, add it to the plan
					// for this gap
					planGap.Blocks = append(planGap.Blocks, block)
				}
			}

			// ensure we sort blocks so deduping iterator works as expected
			sort.Slice(planGap.Blocks, func(i, j int) bool {
				return planGap.Blocks[i].Bounds.Less(planGap.Blocks[j].Bounds)
			})

			peekingBlocks := iter.NewPeekIter[bloomshipper.BlockRef](
				iter.NewSliceIter[bloomshipper.BlockRef](
					planGap.Blocks,
				),
			)
			// dedupe blocks which could be in multiple metas
			itr := iter.NewDedupingIter[bloomshipper.BlockRef, bloomshipper.BlockRef](
				func(a, b bloomshipper.BlockRef) bool {
					return a == b
				},
				iter.Identity[bloomshipper.BlockRef],
				func(a, _ bloomshipper.BlockRef) bloomshipper.BlockRef {
					return a
				},
				peekingBlocks,
			)

			deduped, err := iter.Collect[bloomshipper.BlockRef](itr)
			if err != nil {
				return nil, fmt.Errorf("failed to dedupe blocks: %w", err)
			}
			planGap.Blocks = deduped

			plan.gaps = append(plan.gaps, planGap)
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

func (p *Planner) addPendingTask(task *QueueTask) {
	p.pendingTasks.Store(task.ID, task)
}

func (p *Planner) removePendingTask(task *QueueTask) {
	p.pendingTasks.Delete(task.ID)
}

func (p *Planner) totalPendingTasks() (total int) {
	p.pendingTasks.Range(func(_, _ interface{}) bool {
		total++
		return true
	})
	return total
}

func (p *Planner) enqueueTask(task *QueueTask) error {
	p.activeUsers.UpdateUserTimestamp(task.Tenant, time.Now())
	return p.tasksQueue.Enqueue(task.Tenant, nil, task, func() {
		task.timesEnqueued.Add(1)
		p.addPendingTask(task)
	})
}

func (p *Planner) NotifyBuilderShutdown(
	_ context.Context,
	req *protos.NotifyBuilderShutdownRequest,
) (*protos.NotifyBuilderShutdownResponse, error) {
	level.Debug(p.logger).Log("msg", "builder shutdown", "builder", req.BuilderID)
	p.tasksQueue.NotifyConsumerShutdown(req.GetBuilderID())

	return &protos.NotifyBuilderShutdownResponse{}, nil
}

func (p *Planner) BuilderLoop(builder protos.PlannerForBuilder_BuilderLoopServer) error {
	resp, err := builder.Recv()
	if err != nil {
		return fmt.Errorf("error receiving message from builder: %w", err)
	}

	builderID := resp.GetBuilderID()
	logger := log.With(p.logger, "builder", builderID)

	if !p.isLeader() {
		return errPlannerIsNotLeader
	}

	level.Debug(logger).Log("msg", "builder connected")

	p.tasksQueue.RegisterConsumerConnection(builderID)
	defer p.tasksQueue.UnregisterConsumerConnection(builderID)

	lastIndex := queue.StartIndex
	for p.isRunningOrStopping() {
		item, idx, err := p.tasksQueue.Dequeue(builder.Context(), lastIndex, builderID)
		if err != nil {
			if errors.Is(err, queue.ErrStopped) {
				// Planner is stopping, break the loop and return
				break
			}
			return fmt.Errorf("error dequeuing task: %w", err)
		}
		lastIndex = idx

		if item == nil {

			return fmt.Errorf("dequeue() call resulted in nil response. builder: %s", builderID)
		}
		task := item.(*QueueTask)
		logger := log.With(logger, "task", task.ID)

		queueTime := time.Since(task.queueTime)
		p.metrics.queueDuration.Observe(queueTime.Seconds())

		if task.ctx.Err() != nil {
			level.Warn(logger).Log("msg", "task context done after dequeue", "err", task.ctx.Err())
			lastIndex = lastIndex.ReuseLastIndex()
			p.removePendingTask(task)
			continue
		}

		result, err := p.forwardTaskToBuilder(builder, builderID, task)
		if err != nil {
			maxRetries := p.limits.BloomTaskMaxRetries(task.Tenant)
			if maxRetries > 0 && int(task.timesEnqueued.Load()) >= maxRetries {
				p.removePendingTask(task)
				level.Error(logger).Log(
					"msg", "task failed after max retries",
					"retries", task.timesEnqueued.Load(),
					"maxRetries", maxRetries,
					"err", err,
				)
				task.resultsChannel <- &protos.TaskResult{
					TaskID: task.ID,
					Error:  fmt.Errorf("task failed after max retries (%d): %w", maxRetries, err),
				}
				continue
			}

			// Re-queue the task if the builder is failing to process the tasks
			if err := p.enqueueTask(task); err != nil {
				p.metrics.taskLost.Inc()
				p.removePendingTask(task)
				level.Error(logger).Log("msg", "error re-enqueuing task. this task will be lost", "err", err)
				task.resultsChannel <- &protos.TaskResult{
					TaskID: task.ID,
					Error:  fmt.Errorf("error re-enqueuing task: %w", err),
				}
				continue
			}

			p.metrics.tasksRequeued.Inc()
			level.Error(logger).Log(
				"msg", "error forwarding task to builder, Task requeued",
				"retries", task.timesEnqueued.Load(),
				"err", err,
			)
			continue
		}

		level.Debug(logger).Log(
			"msg", "task completed",
			"duration", time.Since(task.queueTime).Seconds(),
			"retries", task.timesEnqueued.Load()-1, // -1 because the first enqueue is not a retry
		)
		p.removePendingTask(task)

		// Send the result back to the task. The channel is buffered, so this should not block.
		task.resultsChannel <- result
	}

	return errPlannerIsNotRunning
}

func (p *Planner) forwardTaskToBuilder(
	builder protos.PlannerForBuilder_BuilderLoopServer,
	builderID string,
	task *QueueTask,
) (*protos.TaskResult, error) {
	msg := &protos.PlannerToBuilder{
		Task: task.ToProtoTask(),
	}

	if err := builder.Send(msg); err != nil {
		return nil, fmt.Errorf("error sending task to builder (%s): %w", builderID, err)
	}

	// Launch a goroutine to wait for the response from the builder so we can
	// wait for a timeout, or a response from the builder
	resultsCh := make(chan *protos.TaskResult)
	errCh := make(chan error)
	go func() {
		result, err := p.receiveResultFromBuilder(builder, builderID, task)
		if err != nil {
			errCh <- err
			return
		}

		resultsCh <- result
	}()

	timeout := make(<-chan time.Time)
	taskTimeout := p.limits.BuilderResponseTimeout(task.Tenant)
	if taskTimeout != 0 {
		// If the timeout is not 0 (disabled), configure it
		timeout = time.After(taskTimeout)
	}

	select {
	case result := <-resultsCh:
		// Note: Errors from the result are not returned here since we don't retry tasks
		// that return with an error. I.e. we won't retry errors forwarded from the builder.
		// TODO(salvacorts): Filter and return errors that can be retried.
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-timeout:
		return nil, fmt.Errorf("timeout waiting for response from builder (%s)", builderID)
	}
}

// receiveResultFromBuilder waits for a response from the builder and returns the result and an error if any
// The error will be populated if there is an error receiving the response from the builder, in other words,
// errors on the builder side will not be returned as an error here, but as an error in the TaskResult.
func (p *Planner) receiveResultFromBuilder(
	builder protos.PlannerForBuilder_BuilderLoopServer,
	builderID string,
	task *QueueTask,
) (*protos.TaskResult, error) {
	// If connection is closed, Recv() will return an error
	res, err := builder.Recv()
	if err != nil {
		return nil, fmt.Errorf("error receiving response from builder (%s): %w", builderID, err)
	}
	if res.GetBuilderID() != builderID {
		return nil, fmt.Errorf("unexpected builder ID (%s) in response from builder (%s)", res.GetBuilderID(), builderID)
	}

	result, err := protos.FromProtoTaskResult(&res.Result)
	if err != nil {
		return nil, fmt.Errorf("error processing task result in builder (%s): %w", builderID, err)
	}
	if result.TaskID != task.ID {
		return nil, fmt.Errorf("unexpected task ID (%s) in response from builder (%s). Expected task ID is %s", result.TaskID, builderID, task.ID)
	}

	return result, nil
}

func (p *Planner) isRunningOrStopping() bool {
	st := p.State()
	return st == services.Running || st == services.Stopping
}
