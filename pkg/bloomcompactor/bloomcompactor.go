package bloomcompactor

import (
	"context"
	"fmt"
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
	controller *SimpleBloomController

	// temporary workaround until bloomStore has implemented read/write shipper interface
	bloomStore bloomshipper.Store

	sharding util_ring.TenantSharding

	metrics   *Metrics
	btMetrics *v1.Metrics
}

func New(
	cfg Config,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	clientMetrics storage.ClientMetrics,
	fetcherProvider stores.ChunkFetcherProvider,
	sharding util_ring.TenantSharding,
	limits Limits,
	store bloomshipper.Store,
	logger log.Logger,
	r prometheus.Registerer,
) (*Compactor, error) {
	c := &Compactor{
		cfg:        cfg,
		schemaCfg:  schemaCfg,
		logger:     logger,
		sharding:   sharding,
		limits:     limits,
		bloomStore: store,
	}

	tsdbStore, err := NewTSDBStores(schemaCfg, storeCfg, clientMetrics)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create TSDB store")
	}
	c.tsdbStore = tsdbStore

	// initialize metrics
	c.btMetrics = v1.NewMetrics(prometheus.WrapRegistererWithPrefix("loki_bloom_tokenizer_", r))
	c.metrics = NewMetrics(r, c.btMetrics)

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
	var workersErr error
	var wg sync.WaitGroup
	input := make(chan tenantTableRange)

	tables := c.tables(time.Now())
	level.Debug(c.logger).Log("msg", "loaded tables", "tables", tables.Len())

	tracker, err := newCompactionTracker(tables.Len())
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
	err = multierror.New(workersErr, err, ctx.Err()).Err()

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
	adjustedMin := min(c.cfg.MinTableCompactionPeriod - 1)
	minCompactionPeriod := time.Duration(adjustedMin) * config.ObjectStorageIndexRequiredPeriod
	maxCompactionPeriod := time.Duration(c.cfg.MaxTableCompactionPeriod) * config.ObjectStorageIndexRequiredPeriod

	from := ts.Add(-maxCompactionPeriod).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)
	through := ts.Add(-minCompactionPeriod).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)

	fromDay := config.NewDayTime(model.TimeFromUnixNano(from))
	throughDay := config.NewDayTime(model.TimeFromUnixNano(through))
	level.Debug(c.logger).Log("msg", "loaded tables for compaction", "from", fromDay, "through", throughDay)
	return newDayRangeIterator(fromDay, throughDay, c.schemaCfg)
}

func (c *Compactor) loadWork(
	ctx context.Context,
	tables *dayRangeIterator,
	ch chan<- tenantTableRange,
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
		level.Debug(c.logger).Log("msg", "loaded total tenants", "table", table, "tenants", nTenants)
		tracker.registerTable(table.DayTime, nTenants)

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			c.metrics.tenantsDiscovered.Inc()
			tenant := tenants.At()
			ownershipRanges, owns, err := c.ownsTenant(tenant)
			if err != nil {
				return errors.Wrap(err, "checking tenant ownership")
			}
			level.Debug(c.logger).Log("msg", "enqueueing work for tenant", "tenant", tenant, "table", table, "ranges", len(ownershipRanges), "owns", owns)
			if !owns {
				c.metrics.tenantsSkipped.Inc()
				continue
			}
			c.metrics.tenantsOwned.Inc()

			for _, ownershipRange := range ownershipRanges {
				tt := tenantTableRange{
					tenant:         tenant,
					table:          table,
					ownershipRange: ownershipRange,
					queueTime:      time.Now(),
				}
				tracker.add(&tt)

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
	input <-chan tenantTableRange,
	tracker *compactionTracker,
) error {

	return concurrency.ForEachJob(ctx, c.cfg.WorkerParallelism, c.cfg.WorkerParallelism, func(ctx context.Context, idx int) error {

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case tt, ok := <-input:
				if !ok {
					return nil
				}
				tt.startTime = time.Now()
				c.metrics.tenantsStarted.Inc()
				err := c.compactTenantTable(ctx, tt)
				tt.finished = true
				tt.endTime = time.Now()
				duration := tt.endTime.Sub(tt.startTime)
				c.metrics.timePerTenant.WithLabelValues(tt.tenant).Add(duration.Seconds())
				c.metrics.progress.Set(tracker.progress())

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
				c.metrics.tenantTableRanges.WithLabelValues(statusSuccess).Inc()
			}
		}

	})

}

func (c *Compactor) compactTenantTable(ctx context.Context, tt tenantTableRange) error {
	level.Info(c.logger).Log("msg", "compacting", "org_id", tt.tenant, "table", tt.table, "ownership", tt.ownershipRange.String())
	return c.controller.compactTenant(ctx, tt.table, tt.tenant, tt.ownershipRange)
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

func (r *dayRangeIterator) Len() int {
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

type compactionTracker struct {
	sync.RWMutex

	nTables int
	// tables -> n_tenants
	metadata map[config.DayTime]int

	// table -> tenant -> []keyspace
	tables map[config.DayTime]map[string][]*tenantTableRange
}

func newCompactionTracker(nTables int) (*compactionTracker, error) {
	if nTables <= 0 {
		return nil, errors.New("nTables must be positive")
	}

	return &compactionTracker{
		nTables: nTables,
		tables:  make(map[config.DayTime]map[string][]*tenantTableRange),
	}, nil
}

func (c *compactionTracker) registerTable(tbl config.DayTime, nTenants int) {
	c.Lock()
	defer c.Unlock()
	c.metadata[tbl] = nTenants
	c.tables[tbl] = make(map[string][]*tenantTableRange)
}

func (c *compactionTracker) add(tt *tenantTableRange) {
	c.Lock()
	defer c.Unlock()
	tbl, ok := c.tables[tt.table.DayTime]
	if !ok {
		panic(fmt.Sprintf("table not registered: %s", tt.table.DayTime.String()))
	}
	tbl[tt.tenant] = append(tbl[tt.tenant], tt)
}

// Returns progress in (0-1) range.
// compaction progress is measured by the following:
// 1. The number of days of data that has been compacted
// as a percentage of the total number of days of data that needs to be compacted.
// 2. Within each day, the number of tenants that have been compacted
// as a percentage of the total number of tenants that need to be compacted.
// 3. Within each tenant, the percent of the keyspaces that have been compacted.
// NB(owen-d): this treats all tenants equally, when this may not be the case wrt
// the work they have to do. This is a simplification and can be x-referenced with
// the tenant_compaction_seconds_total metric to see how much time is being spent on
// each tenant while the compaction tracker shows total compaction progress across
// all tables and tenants.
func (c *compactionTracker) progress() (progress float64) {
	c.RLock()
	defer c.RUnlock()

	perTablePct := 1. / float64(c.nTables)

	// for all registered tables, determine the number of registered tenants
	for tbl, nTenants := range c.metadata {
		perTenantPct := perTablePct / float64(nTenants)

		// iterate tenants in each table
		for _, tenant := range c.tables[tbl] {
			var (
				totalKeyspace    uint64
				finishedKeyspace uint64
			)

			// iterate table ranges for each tenant+table pair
			for _, tt := range tenant {
				totalKeyspace += tt.ownershipRange.Range()
				if tt.finished {
					finishedKeyspace += tt.ownershipRange.Range()
				}
			}

			tenantProgress := float64(totalKeyspace-finishedKeyspace) / float64(totalKeyspace)
			progress += perTenantPct * tenantProgress
		}
	}

	return progress
}
