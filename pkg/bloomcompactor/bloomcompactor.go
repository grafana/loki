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
	logger log.Logger,
	r prometheus.Registerer,
) (*Compactor, error) {
	c := &Compactor{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		logger:    logger,
		sharding:  sharding,
		limits:    limits,
	}

	tsdbStore, err := NewTSDBStores(schemaCfg, storeCfg, clientMetrics)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create TSDB store")
	}
	c.tsdbStore = tsdbStore

	// TODO(owen-d): export bloomstore as a dependency that can be reused by the compactor & gateway rather that
	bloomStore, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storeCfg, clientMetrics, nil, nil, logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create bloom store")
	}
	c.bloomStore = bloomStore

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
			return ctx.Err()

		case start := <-ticker.C:
			c.metrics.compactionsStarted.Inc()
			if err := c.runOne(ctx); err != nil {
				level.Error(c.logger).Log("msg", "compaction iteration failed", "err", err, "duration", time.Since(start))
				c.metrics.compactionCompleted.WithLabelValues(statusFailure).Inc()
				c.metrics.compactionTime.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
				return err
			}
			level.Info(c.logger).Log("msg", "compaction iteration completed", "duration", time.Since(start))
			c.metrics.compactionCompleted.WithLabelValues(statusSuccess).Inc()
			c.metrics.compactionTime.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
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

type tenantTable struct {
	tenant         string
	table          config.DayTable
	ownershipRange v1.FingerprintBounds
}

func (c *Compactor) tenants(ctx context.Context, table config.DayTable) (v1.Iterator[string], error) {
	tenants, err := c.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, errors.Wrap(err, "getting tenants")
	}

	return v1.NewSliceIter(tenants), nil
}

// ownsTenant returns the ownership range for the tenant, if the compactor owns the tenant, and an error.
func (c *Compactor) ownsTenant(tenant string) (v1.FingerprintBounds, bool, error) {
	tenantRing, owned := c.sharding.OwnsTenant(tenant)
	if !owned {
		return v1.FingerprintBounds{}, false, nil
	}

	rs, err := tenantRing.GetAllHealthy(RingOp)
	if err != nil {
		return v1.FingerprintBounds{}, false, errors.Wrap(err, "getting ring healthy instances")

	}

	keyRange, err := bloomutils.KeyRangeForInstance(c.cfg.Ring.InstanceID, rs.Instances, bloomutils.Uint64Range)
	if err != nil {
		return v1.FingerprintBounds{}, false, errors.Wrap(err, "getting instance token range")
	}
	return v1.NewBounds(model.Fingerprint(keyRange.Min), model.Fingerprint(keyRange.Max)), true, nil
}

// runs a single round of compaction for all relevant tenants and tables
func (c *Compactor) runOne(ctx context.Context) error {
	level.Info(c.logger).Log("msg", "running bloom compaction", "workers", c.cfg.WorkerParallelism)
	var workersErr error
	var wg sync.WaitGroup
	ch := make(chan tenantTable)
	wg.Add(1)
	go func() {
		workersErr = c.runWorkers(ctx, ch)
		wg.Done()
	}()

	err := c.loadWork(ctx, ch)

	wg.Wait()
	err = multierror.New(workersErr, err, ctx.Err()).Err()
	if err != nil {
		level.Error(c.logger).Log("msg", "compaction iteration failed", "err", err)
	}
	return err
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

func (c *Compactor) loadWork(ctx context.Context, ch chan<- tenantTable) error {
	tables := c.tables(time.Now())

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {
		table := tables.At()

		level.Debug(c.logger).Log("msg", "loading work for table", "table", table)

		tenants, err := c.tenants(ctx, table)
		if err != nil {
			return errors.Wrap(err, "getting tenants")
		}

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			c.metrics.tenantsDiscovered.Inc()
			tenant := tenants.At()
			ownershipRange, owns, err := c.ownsTenant(tenant)
			if err != nil {
				return errors.Wrap(err, "checking tenant ownership")
			}
			level.Debug(c.logger).Log("msg", "enqueueing work for tenant", "tenant", tenant, "table", table, "ownership", ownershipRange.String(), "owns", owns)
			if !owns {
				c.metrics.tenantsSkipped.Inc()
				continue
			}
			c.metrics.tenantsOwned.Inc()

			select {
			case ch <- tenantTable{
				tenant:         tenant,
				table:          table,
				ownershipRange: ownershipRange,
			}:
			case <-ctx.Done():
				return ctx.Err()
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

func (c *Compactor) runWorkers(ctx context.Context, ch <-chan tenantTable) error {

	return concurrency.ForEachJob(ctx, c.cfg.WorkerParallelism, c.cfg.WorkerParallelism, func(ctx context.Context, idx int) error {

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case tt, ok := <-ch:
				if !ok {
					return nil
				}

				start := time.Now()
				c.metrics.tenantsStarted.Inc()
				if err := c.compactTenantTable(ctx, tt); err != nil {
					c.metrics.tenantsCompleted.WithLabelValues(statusFailure).Inc()
					c.metrics.tenantsCompletedTime.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
					return errors.Wrapf(
						err,
						"compacting tenant table (%s) for tenant (%s) with ownership (%s)",
						tt.table,
						tt.tenant,
						tt.ownershipRange,
					)
				}
				c.metrics.tenantsCompleted.WithLabelValues(statusSuccess).Inc()
				c.metrics.tenantsCompletedTime.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
			}
		}

	})

}

func (c *Compactor) compactTenantTable(ctx context.Context, tt tenantTable) error {
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
