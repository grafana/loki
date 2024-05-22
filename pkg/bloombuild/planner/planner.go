package planner

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/util"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

type Planner struct {
	services.Service
	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	cfg       Config
	limits    Limits
	schemaCfg config.SchemaConfig

	tsdbStore  TSDBStore
	bloomStore bloomshipper.Store

	tasksQueue  *queue.RequestQueue
	activeUsers *util.ActiveUsersCleanupService

	metrics *Metrics
	logger  log.Logger
}

func New(
	cfg Config,
	limits Limits,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	storageMetrics storage.ClientMetrics,
	bloomStore bloomshipper.Store,
	logger log.Logger,
	r prometheus.Registerer,
) (*Planner, error) {
	utillog.WarnExperimentalUse("Bloom Planner", logger)

	tsdbStore, err := NewTSDBStores(schemaCfg, storeCfg, storageMetrics, logger)
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

	svcs := []services.Service{p.tasksQueue, p.activeUsers}
	p.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return nil, fmt.Errorf("error creating subservices manager: %w", err)
	}
	p.subservicesWatcher = services.NewFailureWatcher()
	p.subservicesWatcher.WatchManager(p.subservices)

	p.Service = services.NewBasicService(p.starting, p.running, p.stopping)
	return p, nil
}

func (p *Planner) starting(_ context.Context) (err error) {
	p.metrics.running.Set(1)
	return err
}

func (p *Planner) stopping(_ error) error {
	p.metrics.running.Set(0)
	return nil
}

func (p *Planner) running(ctx context.Context) error {
	// run once at beginning
	if err := p.runOne(ctx); err != nil {
		level.Error(p.logger).Log("msg", "bloom build iteration failed for the first time", "err", err)
	}

	ticker := time.NewTicker(p.cfg.PlanningInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			level.Debug(p.logger).Log("msg", "planner context done", "err", err)
			return err

		case <-ticker.C:
			level.Info(p.logger).Log("msg", "starting bloom build iteration")
			if err := p.runOne(ctx); err != nil {
				level.Error(p.logger).Log("msg", "bloom build iteration failed", "err", err)
			}
		}
	}
}

func (p *Planner) runOne(ctx context.Context) error {
	var (
		start  = time.Now()
		status = statusFailure
	)
	defer func() {
		p.metrics.buildCompleted.WithLabelValues(status).Inc()
		p.metrics.buildTime.WithLabelValues(status).Observe(time.Since(start).Seconds())
	}()

	p.metrics.buildStarted.Inc()

	tables := p.tables(time.Now())
	level.Debug(p.logger).Log("msg", "loaded tables", "tables", tables.TotalDays())

	work, err := p.loadWork(ctx, tables)
	if err != nil {
		return fmt.Errorf("error loading work: %w", err)
	}

	var totalTasks int
	for _, w := range work {
		logger := log.With(p.logger, "tenant", w.tenant, "table", w.table.Addr(), "ownership", w.ownershipRange.String())

		gaps, err := p.findGapsForBounds(ctx, w.tenant, w.table, w.ownershipRange)
		if err != nil {
			level.Error(logger).Log("msg", "error finding gaps", "err", err)
			continue
		}

		now := time.Now()
		for _, gap := range gaps {
			totalTasks++
			task := Task{
				table:           w.table.Addr(),
				tenant:          w.tenant,
				OwnershipBounds: w.ownershipRange,
				tsdb:            gap.tsdb,
				gaps:            gap.gaps,

				queueTime: now,
				ctx:       ctx,
			}

			p.activeUsers.UpdateUserTimestamp(task.tenant, now)
			if err := p.tasksQueue.Enqueue(task.tenant, nil, task, nil); err != nil {
				level.Error(logger).Log("msg", "error enqueuing task", "err", err)
				continue
			}
		}
	}

	level.Debug(p.logger).Log("msg", "planning completed", "tasks", totalTasks)

	status = statusSuccess
	level.Info(p.logger).Log(
		"msg", "bloom build iteration completed",
		"duration", time.Since(start).Seconds(),
	)
	return nil
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

type tenantTableRange struct {
	tenant         string
	table          config.DayTable
	ownershipRange v1.FingerprintBounds

	// TODO: Add tracking
	//finished                      bool
	//queueTime, startTime, endTime time.Time
}

func (p *Planner) loadWork(
	ctx context.Context,
	tables *dayRangeIterator,
) ([]tenantTableRange, error) {
	var work []tenantTableRange

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {
		table := tables.At()
		level.Debug(p.logger).Log("msg", "loading work for table", "table", table)

		tenants, err := p.tenants(ctx, table)
		if err != nil {
			return nil, fmt.Errorf("error loading tenants: %w", err)
		}
		level.Debug(p.logger).Log("msg", "loaded tenants", "table", table, "tenants", tenants.Len())

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			p.metrics.tenantsDiscovered.Inc()
			tenant := tenants.At()

			if !p.limits.BloomCreationEnabled(tenant) {
				continue
			}

			splitFactor := p.limits.BloomSplitSeriesKeyspaceBy(tenant)
			bounds := SplitFingerprintKeyspaceByFactor(splitFactor)

			for _, bounds := range bounds {
				work = append(work, tenantTableRange{
					tenant:         tenant,
					table:          table,
					ownershipRange: bounds,
				})
			}

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

	return work, ctx.Err()
}

func (p *Planner) tenants(ctx context.Context, table config.DayTable) (*v1.SliceIter[string], error) {
	tenants, err := p.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, fmt.Errorf("error loading tenants for table (%s): %w", table, err)
	}

	return v1.NewSliceIter(tenants), nil
}

/*
Planning works as follows, split across many functions for clarity:
 1. Fetch all meta.jsons for the given tenant and table which overlap the ownership range of this compactor.
 2. Load current TSDBs for this tenant/table.
 3. For each live TSDB (there should be only 1, but this works with multiple), find any gaps
    (fingerprint ranges) which are not up-to-date, determined by checking other meta.json files and comparing
    the TSDBs they were generated from as well as their ownership ranges.
*/
func (p *Planner) findGapsForBounds(
	ctx context.Context,
	tenant string,
	table config.DayTable,
	ownershipRange v1.FingerprintBounds,
) ([]blockPlan, error) {
	logger := log.With(p.logger, "org_id", tenant, "table", table.Addr(), "ownership", ownershipRange.String())

	// Fetch source metas to be used in both build and cleanup of out-of-date metas+blooms
	metas, err := p.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.NewInterval(table.Bounds()),
			Keyspace: ownershipRange,
		},
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to get metas", "err", err)
		return nil, fmt.Errorf("failed to get metas: %w", err)
	}

	level.Debug(logger).Log("msg", "found relevant metas", "metas", len(metas))

	// Find gaps in the TSDBs for this tenant/table
	gaps, err := p.findOutdatedGaps(ctx, tenant, table, ownershipRange, metas, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to find outdated gaps: %w", err)
	}

	return gaps, nil
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
	gaps []GapWithBlocks
}

func (p *Planner) findOutdatedGaps(
	ctx context.Context,
	tenant string,
	table config.DayTable,
	ownershipRange v1.FingerprintBounds,
	metas []bloomshipper.Meta,
	logger log.Logger,
) ([]blockPlan, error) {
	// Resolve TSDBs
	tsdbs, err := p.tsdbStore.ResolveTSDBs(ctx, table, tenant)
	if err != nil {
		level.Error(logger).Log("msg", "failed to resolve tsdbs", "err", err)
		return nil, fmt.Errorf("failed to resolve tsdbs: %w", err)
	}

	if len(tsdbs) == 0 {
		return nil, nil
	}

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

	work, err := blockPlansForGaps(tsdbsWithGaps, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create plan", "err", err)
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	return work, nil
}

// Used to signal the gaps that need to be populated for a tsdb
type tsdbGaps struct {
	tsdb tsdb.SingleTenantTSDBIdentifier
	gaps []v1.FingerprintBounds
}

// gapsBetweenTSDBsAndMetas returns if the metas are up-to-date with the TSDBs. This is determined by asserting
// that for each TSDB, there are metas covering the entire ownership range which were generated from that specific TSDB.
func gapsBetweenTSDBsAndMetas(
	ownershipRange v1.FingerprintBounds,
	tsdbs []tsdb.SingleTenantTSDBIdentifier,
	metas []bloomshipper.Meta,
) (res []tsdbGaps, err error) {
	for _, db := range tsdbs {
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
				tsdb: db,
				gaps: gaps,
			})
		}
	}

	return res, err
}

// blockPlansForGaps groups tsdb gaps we wish to fill with overlapping but out of date blocks.
// This allows us to expedite bloom generation by using existing blocks to fill in the gaps
// since many will contain the same chunks.
func blockPlansForGaps(tsdbs []tsdbGaps, metas []bloomshipper.Meta) ([]blockPlan, error) {
	plans := make([]blockPlan, 0, len(tsdbs))

	for _, idx := range tsdbs {
		plan := blockPlan{
			tsdb: idx.tsdb,
			gaps: make([]GapWithBlocks, 0, len(idx.gaps)),
		}

		for _, gap := range idx.gaps {
			planGap := GapWithBlocks{
				bounds: gap,
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
					planGap.blocks = append(planGap.blocks, block)
				}
			}

			// ensure we sort blocks so deduping iterator works as expected
			sort.Slice(planGap.blocks, func(i, j int) bool {
				return planGap.blocks[i].Bounds.Less(planGap.blocks[j].Bounds)
			})

			peekingBlocks := v1.NewPeekingIter[bloomshipper.BlockRef](
				v1.NewSliceIter[bloomshipper.BlockRef](
					planGap.blocks,
				),
			)
			// dedupe blocks which could be in multiple metas
			itr := v1.NewDedupingIter[bloomshipper.BlockRef, bloomshipper.BlockRef](
				func(a, b bloomshipper.BlockRef) bool {
					return a == b
				},
				v1.Identity[bloomshipper.BlockRef],
				func(a, _ bloomshipper.BlockRef) bloomshipper.BlockRef {
					return a
				},
				peekingBlocks,
			)

			deduped, err := v1.Collect[bloomshipper.BlockRef](itr)
			if err != nil {
				return nil, fmt.Errorf("failed to dedupe blocks: %w", err)
			}
			planGap.blocks = deduped

			plan.gaps = append(plan.gaps, planGap)
		}

		plans = append(plans, plan)
	}

	return plans, nil
}
