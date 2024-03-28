package bloomcompactor

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/bloomutils"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	util_ring "github.com/grafana/loki/pkg/util/ring"
)

var (
	RingOp = ring.NewOp([]ring.InstanceState{ring.JOINING, ring.ACTIVE}, nil)
)

/*
Bloom-compactor

This is a standalone service that is responsible for compacting TSDB indexes into bloomfilters.
It creates and merges bloomfilters into an aggregated form, called bloom-blocks.
It maintains a list of references between bloom-blocks and TSDB indexes in files called meta.jsons.

Bloom-compactor regularly runs to check for changes in meta.jsons and runs compaction only upon changes in TSDBs.
*/
type Compactor struct {
	services.Service

	cfg       Config
	schemaCfg config.SchemaConfig
	logger    log.Logger
	limits    Limits

	tsdbStore TSDBStore
	// TODO(owen-d): ShardingStrategy
	controller       *SimpleBloomController
	retentionManager *RetentionManager

	// temporary workaround until bloomStore has implemented read/write shipper interface
	bloomStore bloomshipper.Store

	sharding util_ring.TenantSharding

	metrics *Metrics
}

func New(
	cfg Config,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	clientMetrics storage.ClientMetrics,
	fetcherProvider stores.ChunkFetcherProvider,
	ring ring.ReadRing,
	ringLifeCycler *ring.BasicLifecycler,
	limits Limits,
	store bloomshipper.StoreWithMetrics,
	logger log.Logger,
	r prometheus.Registerer,
) (*Compactor, error) {
	c := &Compactor{
		cfg:        cfg,
		schemaCfg:  schemaCfg,
		logger:     logger,
		sharding:   util_ring.NewTenantShuffleSharding(ring, ringLifeCycler, limits.BloomCompactorShardSize),
		limits:     limits,
		bloomStore: store,
		metrics:    NewMetrics(r, store.BloomMetrics()),
	}

	tsdbStore, err := NewTSDBStores(schemaCfg, storeCfg, clientMetrics, logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create TSDB store")
	}
	c.tsdbStore = tsdbStore

	chunkLoader := NewStoreChunkLoader(
		fetcherProvider,
		c.metrics,
	)

	c.controller = NewSimpleBloomController(
		c.tsdbStore,
		c.bloomStore,
		chunkLoader,
		c.limits,
		c.metrics,
		c.logger,
	)

	c.retentionManager = NewRetentionManager(
		c.cfg.RetentionConfig,
		c.limits,
		c.bloomStore,
		newFirstTokenRetentionSharding(ring, ringLifeCycler),
		c.metrics,
		c.logger,
	)

	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)
	return c, nil
}

func (c *Compactor) starting(_ context.Context) (err error) {
	c.metrics.compactorRunning.Set(1)
	return err
}

func (c *Compactor) stopping(_ error) error {
	c.metrics.compactorRunning.Set(0)
	return nil
}

func (c *Compactor) running(ctx context.Context) error {
	// run once at beginning
	if err := c.runOne(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(c.cfg.CompactionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			level.Debug(c.logger).Log("msg", "compactor context done", "err", err)
			return err

		case <-ticker.C:
			if err := c.runOne(ctx); err != nil {
				return err
			}
		}
	}
}

func runWithRetries(
	ctx context.Context,
	minBackoff, maxBackoff time.Duration,
	maxRetries int,
	f func(ctx context.Context) error,
) error {
	var lastErr error

	retries := backoff.New(ctx, backoff.Config{
		MinBackoff: minBackoff,
		MaxBackoff: maxBackoff,
		MaxRetries: maxRetries,
	})

	for retries.Ongoing() {
		lastErr = f(ctx)
		if lastErr == nil {
			return nil
		}

		retries.Wait()
	}

	return lastErr
}

type tenantTableRange struct {
	tenant         string
	table          config.DayTable
	ownershipRange v1.FingerprintBounds

	finished                      bool
	queueTime, startTime, endTime time.Time
}

func (c *Compactor) tenants(ctx context.Context, table config.DayTable) (*v1.SliceIter[string], error) {
	tenants, err := c.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, errors.Wrap(err, "getting tenants")
	}

	return v1.NewSliceIter(tenants), nil
}

// ownsTenant returns the ownership range for the tenant, if the compactor owns the tenant, and an error.
func (c *Compactor) ownsTenant(tenant string) ([]v1.FingerprintBounds, bool, error) {
	if !c.limits.BloomCompactorEnabled(tenant) {
		return nil, false, nil
	}
	tenantRing, owned := c.sharding.OwnsTenant(tenant)
	if !owned {
		return nil, false, nil
	}

	// TODO(owen-d): use <ReadRing>.GetTokenRangesForInstance()
	// when it's supported for non zone-aware rings
	// instead of doing all this manually

	rs, err := tenantRing.GetAllHealthy(RingOp)
	if err != nil {
		return nil, false, errors.Wrap(err, "getting ring healthy instances")
	}

	ranges, err := bloomutils.TokenRangesForInstance(c.cfg.Ring.InstanceID, rs.Instances)
	if err != nil {
		return nil, false, errors.Wrap(err, "getting token ranges for instance")
	}

	keyspaces := bloomutils.KeyspacesFromTokenRanges(ranges)
	return keyspaces, true, nil
}

// runs a single round of compaction for all relevant tenants and tables
func (c *Compactor) runOne(ctx context.Context) error {
	c.metrics.compactionsStarted.Inc()
	start := time.Now()
	level.Info(c.logger).Log("msg", "running bloom compaction", "workers", c.cfg.WorkerParallelism)
	var workersErr, retentionErr error
	var wg sync.WaitGroup
	input := make(chan *tenantTableRange)

	// Launch retention (will return instantly if retention is disabled or not owned by this compactor)
	wg.Add(1)
	go func() {
		retentionErr = c.retentionManager.Apply(ctx)
		wg.Done()
	}()

	tables := c.tables(time.Now())
	level.Debug(c.logger).Log("msg", "loaded tables", "tables", tables.TotalDays())

	tracker, err := newCompactionTracker(tables.TotalDays())
	if err != nil {
		return errors.Wrap(err, "creating compaction tracker")
	}

	wg.Add(1)
	go func() {
		workersErr = c.runWorkers(ctx, input, tracker)
		wg.Done()
	}()

	err = c.loadWork(ctx, tables, input, tracker)

	wg.Wait()
	duration := time.Since(start)
	err = multierror.New(retentionErr, workersErr, err, ctx.Err()).Err()

	if err != nil {
		level.Error(c.logger).Log("msg", "compaction iteration failed", "err", err, "duration", duration)
		c.metrics.compactionCompleted.WithLabelValues(statusFailure).Inc()
		c.metrics.compactionTime.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
		return err
	}

	c.metrics.compactionCompleted.WithLabelValues(statusSuccess).Inc()
	c.metrics.compactionTime.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
	level.Info(c.logger).Log("msg", "compaction iteration completed", "duration", duration)
	return nil
}

func (c *Compactor) tables(ts time.Time) *dayRangeIterator {
	// adjust the minimum by one to make it inclusive, which is more intuitive
	// for a configuration variable
	adjustedMin := min(c.cfg.MinTableOffset - 1)
	minCompactionDelta := time.Duration(adjustedMin) * config.ObjectStorageIndexRequiredPeriod
	maxCompactionDelta := time.Duration(c.cfg.MaxTableOffset) * config.ObjectStorageIndexRequiredPeriod

	from := ts.Add(-maxCompactionDelta).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)
	through := ts.Add(-minCompactionDelta).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)

	fromDay := config.NewDayTime(model.TimeFromUnixNano(from))
	throughDay := config.NewDayTime(model.TimeFromUnixNano(through))
	level.Debug(c.logger).Log("msg", "loaded tables for compaction", "from", fromDay, "through", throughDay)
	return newDayRangeIterator(fromDay, throughDay, c.schemaCfg)
}

func (c *Compactor) loadWork(
	ctx context.Context,
	tables *dayRangeIterator,
	ch chan<- *tenantTableRange,
	tracker *compactionTracker,
) error {

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {
		table := tables.At()

		level.Debug(c.logger).Log("msg", "loading work for table", "table", table)

		tenants, err := c.tenants(ctx, table)
		if err != nil {
			return errors.Wrap(err, "getting tenants")
		}
		nTenants := tenants.Len()

		type ownedTenant struct {
			tenant          string
			ownershipRanges []v1.FingerprintBounds
		}

		// build owned tenants separately and load them all prior to compaction in order to
		// accurately report progress
		var ownedTenants []ownedTenant

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			c.metrics.tenantsDiscovered.Inc()
			tenant := tenants.At()
			ownershipRanges, owns, err := c.ownsTenant(tenant)
			if err != nil {
				return errors.Wrap(err, "checking tenant ownership")
			}
			if !owns {
				level.Debug(c.logger).Log("msg", "skipping tenant", "tenant", tenant, "table", table)
				c.metrics.tenantsSkipped.Inc()
				continue
			}
			c.metrics.tenantsOwned.Inc()
			ownedTenants = append(ownedTenants, ownedTenant{tenant, ownershipRanges})
		}
		if err := tenants.Err(); err != nil {
			level.Error(c.logger).Log("msg", "error iterating tenants", "err", err)
			return errors.Wrap(err, "iterating tenants")
		}

		level.Debug(c.logger).Log("msg", "loaded tenants", "table", table, "tenants", nTenants, "owned_tenants", len(ownedTenants))
		tracker.registerTable(table.DayTime, len(ownedTenants))

		for _, t := range ownedTenants {
			// loop over ranges, registering them in the tracker;
			// we add them to the tracker before queueing them
			// so progress reporting is aware of all tenant/table
			// pairs prior to execution. Otherwise, progress could
			// decrease over time as more work is discovered.
			var inputs []*tenantTableRange
			for _, ownershipRange := range t.ownershipRanges {
				tt := tenantTableRange{
					tenant:         t.tenant,
					table:          table,
					ownershipRange: ownershipRange,
				}
				tracker.update(tt.tenant, tt.table.DayTime, tt.ownershipRange, tt.ownershipRange.Min)
				inputs = append(inputs, &tt)
			}

			// iterate the inputs, queueing them
			for _, tt := range inputs {
				level.Debug(c.logger).Log("msg", "enqueueing work for tenant", "tenant", tt.tenant, "table", table, "ownership", tt.ownershipRange.String())
				tt.queueTime = time.Now() // accurrately report queue time
				select {
				case ch <- tt:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		if err := tenants.Err(); err != nil {
			level.Error(c.logger).Log("msg", "error iterating tenants", "err", err)
			return errors.Wrap(err, "iterating tenants")
		}

	}

	if err := tables.Err(); err != nil {
		level.Error(c.logger).Log("msg", "error iterating tables", "err", err)
		return errors.Wrap(err, "iterating tables")
	}

	close(ch)
	return ctx.Err()
}

func (c *Compactor) runWorkers(
	ctx context.Context,
	input <-chan *tenantTableRange,
	tracker *compactionTracker,
) error {

	// TODO(owen-d): refactor for cleanliness
	reporterCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ticker.C:
				c.metrics.progress.Set(tracker.progress())
			case <-reporterCtx.Done():
				c.metrics.progress.Set(tracker.progress())
				wg.Done()
				ticker.Stop()
				return
			}
		}
	}()

	err := concurrency.ForEachJob(ctx, c.cfg.WorkerParallelism, c.cfg.WorkerParallelism, func(ctx context.Context, idx int) error {

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case tt, ok := <-input:
				if !ok {
					return nil
				}
				c.metrics.tenantsStarted.Inc()
				err := c.compactTenantTable(ctx, tt, tracker)
				duration := tt.endTime.Sub(tt.startTime)
				c.metrics.timePerTenant.WithLabelValues(tt.tenant).Add(duration.Seconds())
				progress := tracker.progress()

				if err != nil {
					c.metrics.tenantTableRanges.WithLabelValues(statusFailure).Inc()
					return errors.Wrapf(
						err,
						"compacting tenant table (%s) for tenant (%s) with ownership (%s)",
						tt.table,
						tt.tenant,
						tt.ownershipRange,
					)
				}
				level.Debug(c.logger).Log(
					"msg", "finished compacting tenant table",
					"tenant", tt.tenant,
					"table", tt.table,
					"ownership", tt.ownershipRange.String(),
					"duration", duration,
					"current_progress", progress,
				)
				c.metrics.tenantTableRanges.WithLabelValues(statusSuccess).Inc()
			}
		}

	})
	cancel()
	wg.Wait()

	return err

}

func (c *Compactor) compactTenantTable(ctx context.Context, tt *tenantTableRange, tracker *compactionTracker) error {
	level.Info(c.logger).Log("msg", "compacting", "org_id", tt.tenant, "table", tt.table, "ownership", tt.ownershipRange.String())
	tt.startTime = time.Now()
	err := c.controller.compactTenant(ctx, tt.table, tt.tenant, tt.ownershipRange, tracker)
	tt.finished = true
	tt.endTime = time.Now()
	tracker.update(tt.tenant, tt.table.DayTime, tt.ownershipRange, tt.ownershipRange.Max)
	level.Info(c.logger).Log("msg", "finished compacting", "org_id", tt.tenant, "table", tt.table, "ownership", tt.ownershipRange.String(), "err", err)
	return err
}

type dayRangeIterator struct {
	min, max, cur config.DayTime
	curPeriod     config.PeriodConfig
	schemaCfg     config.SchemaConfig
	err           error
}

func newDayRangeIterator(min, max config.DayTime, schemaCfg config.SchemaConfig) *dayRangeIterator {
	return &dayRangeIterator{min: min, max: max, cur: min.Dec(), schemaCfg: schemaCfg}
}

func (r *dayRangeIterator) TotalDays() int {
	offset := r.cur
	if r.cur.Before(r.min) {
		offset = r.min
	}
	return int(r.max.Sub(offset.Time) / config.ObjectStorageIndexRequiredPeriod)
}

func (r *dayRangeIterator) Next() bool {
	r.cur = r.cur.Inc()
	if !r.cur.Before(r.max) {
		return false
	}

	period, err := r.schemaCfg.SchemaForTime(r.cur.ModelTime())
	if err != nil {
		r.err = errors.Wrapf(err, "getting schema for time (%s)", r.cur)
		return false
	}
	r.curPeriod = period

	return true
}

func (r *dayRangeIterator) At() config.DayTable {
	return config.NewDayTable(r.cur, r.curPeriod.IndexTables.Prefix)
}

func (r *dayRangeIterator) Err() error {
	return nil
}
