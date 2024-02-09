package bloomcompactor

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/bloomutils"
	"github.com/grafana/loki/pkg/compactor"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/util"
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

	cfg    Config
	logger log.Logger
	limits Limits

	tsdbStore  TSDBStore
	controller *SimpleBloomController

	// temporary workaround until store has implemented read/write shipper interface
	store bloomshipper.Store

	sharding ShardingStrategy

	metrics   *metrics
	btMetrics *v1.Metrics
}

func New(
	cfg Config,
	store bloomshipper.Store,
	sharding ShardingStrategy,
	limits Limits,
	logger log.Logger,
	r prometheus.Registerer,
) (*Compactor, error) {
	c := &Compactor{
		cfg:      cfg,
		store:    store,
		logger:   logger,
		sharding: sharding,
		limits:   limits,
	}

	// initialize metrics
	c.btMetrics = v1.NewMetrics(prometheus.WrapRegistererWithPrefix("loki_bloom_tokenizer", r))
	c.metrics = newMetrics(r)
	c.metrics.compactionRunInterval.Set(cfg.CompactionInterval.Seconds())
	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)

	return c, nil
}

func (c *Compactor) starting(_ context.Context) (err error) {
	c.metrics.compactorRunning.Set(1)
	return err
}

func (c *Compactor) running(ctx context.Context) error {
	// Run an initial compaction before starting the interval.
	if err := c.runCompaction(ctx); err != nil {
		level.Error(c.logger).Log("msg", "failed to run compaction", "err", err)
	}

	ticker := time.NewTicker(util.DurationWithJitter(c.cfg.CompactionInterval, 0.05))
	defer ticker.Stop()

	for {
		select {
		case start := <-ticker.C:
			c.metrics.compactionRunsStarted.Inc()
			if err := c.runCompaction(ctx); err != nil {
				c.metrics.compactionRunsCompleted.WithLabelValues(statusFailure).Inc()
				c.metrics.compactionRunTime.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
				level.Error(c.logger).Log("msg", "failed to run compaction", "err", err)
				continue
			}
			c.metrics.compactionRunsCompleted.WithLabelValues(statusSuccess).Inc()
			c.metrics.compactionRunTime.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *Compactor) stopping(_ error) error {
	c.metrics.compactorRunning.Set(0)
	return nil
}

func (c *Compactor) runCompaction(ctx context.Context) error {
	var tables []string
	// TODO(owen-d): resolve tables

	// process most recent tables first
	tablesIntervals := getIntervalsForTables(tables)
	compactor.SortTablesByRange(tables)

	// TODO(owen-d): parallelize at the bottom level, not the top level.
	// Can dispatch to a queue & wait.
	for _, table := range tables {
		logger := log.With(c.logger, "table", table)
		err := c.compactTable(ctx, logger, table, tablesIntervals[table])
		if err != nil {
			level.Error(logger).Log("msg", "failed to compact table", "err", err)
			return errors.Wrapf(err, "failed to compact table %s", table)
		}
	}
	return nil
}

func (c *Compactor) compactTable(ctx context.Context, logger log.Logger, tableName string, tableInterval model.Interval) error {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("interrupting compaction of table: %w", err)
	}

	var tenants []string

	level.Info(logger).Log("msg", "discovered tenants from bucket", "users", len(tenants))
	return c.compactUsers(ctx, logger, tableName, tableInterval, tenants)
}

func (c *Compactor) compactUsers(ctx context.Context, logger log.Logger, tableName string, tableInterval model.Interval, tenants []string) error {
	// Keep track of tenants owned by this shard, so that we can delete the local files for all other users.
	errs := multierror.New()
	ownedTenants := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		tenantLogger := log.With(logger, "tenant", tenant)

		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupting compaction of tenants: %w", err)
		}

		// Skip tenant if compaction is not enabled
		if !c.limits.BloomCompactorEnabled(tenant) {
			level.Info(tenantLogger).Log("msg", "compaction disabled for tenant. Skipping.")
			continue
		}

		// Skip this table if it is too old for the tenant limits.
		now := model.Now()
		tableMaxAge := c.limits.BloomCompactorMaxTableAge(tenant)
		if tableMaxAge > 0 && tableInterval.Start.Before(now.Add(-tableMaxAge)) {
			level.Debug(tenantLogger).Log("msg", "skipping tenant because table is too old", "table-max-age", tableMaxAge, "table-start", tableInterval.Start, "now", now)
			continue
		}

		// Ensure the tenant ID belongs to our shard.
		if !c.sharding.OwnsTenant(tenant) {
			c.metrics.compactionRunSkippedTenants.Inc()
			level.Debug(tenantLogger).Log("msg", "skipping tenant because it is not owned by this shard")
			continue
		}

		ownedTenants[tenant] = struct{}{}

		start := time.Now()
		if err := c.compactTenantWithRetries(ctx, tenantLogger, tableName, tenant); err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				// We don't want to count shutdowns as failed compactions because we will pick up with the rest of the compaction after the restart.
				level.Info(tenantLogger).Log("msg", "compaction for tenant was interrupted by a shutdown")
				return nil
			default:
				c.metrics.compactionRunTenantsCompleted.WithLabelValues(statusFailure).Inc()
				c.metrics.compactionRunTenantsTime.WithLabelValues(statusFailure).Observe(time.Since(start).Seconds())
				level.Error(tenantLogger).Log("msg", "failed to compact tenant", "err", err)
				errs.Add(err)
			}
			continue
		}

		c.metrics.compactionRunTenantsCompleted.WithLabelValues(statusSuccess).Inc()
		c.metrics.compactionRunTenantsTime.WithLabelValues(statusSuccess).Observe(time.Since(start).Seconds())
		level.Info(tenantLogger).Log("msg", "successfully compacted tenant")
	}

	return errs.Err()

	// TODO: Delete local files for unowned tenants, if there are any.
}

func (c *Compactor) compactTenant(ctx context.Context, logger log.Logger, _ string, tenant string) error {
	level.Info(logger).Log("msg", "starting compaction of tenant")

	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}

	// Tokenizer is not thread-safe so we need one per goroutine.
	nGramLen := c.limits.BloomNGramLength(tenant)
	nGramSkip := c.limits.BloomNGramSkip(tenant)
	_ = v1.NewBloomTokenizer(nGramLen, nGramSkip, c.btMetrics)

	rs, err := c.sharding.GetTenantSubRing(tenant).GetAllHealthy(RingOp)
	if err != nil {
		return err
	}
	tokenRanges := bloomutils.GetInstanceWithTokenRange(c.cfg.Ring.InstanceID, rs.Instances)
	for _, tr := range tokenRanges {
		level.Debug(logger).Log("msg", "got token range for instance", "id", tr.Instance.Id, "min", tr.MinToken, "max", tr.MaxToken)
	}

	// TODO(owen-d): impl
	return nil
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

func (c *Compactor) compactTenantWithRetries(ctx context.Context, logger log.Logger, tableName string, tenant string) error {
	return runWithRetries(
		ctx,
		c.cfg.RetryMinBackoff,
		c.cfg.RetryMaxBackoff,
		c.cfg.CompactionRetries,
		func(ctx context.Context) error {
			return c.compactTenant(ctx, logger, tableName, tenant)
		},
	)
}

type tenantTable struct {
	tenant         string
	table          DayTable
	ownershipRange v1.FingerprintBounds
}

func (c *Compactor) tenants(ctx context.Context, table string) (v1.Iterator[string], error) {
	tenants, err := c.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, errors.Wrap(err, "getting tenants")
	}

	return v1.NewSliceIter(tenants), nil
}

// TODO(owen-d): implement w/ subrings
func (c *Compactor) ownsTenant(_ string) (ownershipRange v1.FingerprintBounds, owns bool) {
	return v1.NewBounds(0, math.MaxUint64), true
}

func (c *Compactor) run(ctx context.Context) error {
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

		case <-ticker.C:
			if err := c.runOne(ctx); err != nil {
				level.Error(c.logger).Log("msg", "compaction iteration failed", "err", err)
				return err
			}
		}
	}
}

// runs a single round of compaction for all relevant tenants and tables
func (c *Compactor) runOne(ctx context.Context) error {
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
	return multierror.New(workersErr, err, ctx.Err()).Err()
}

func (c *Compactor) tables(ts time.Time) *dayRangeIterator {
	from := model.TimeFromUnixNano(ts.Add(-c.cfg.MaxCompactionAge).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod))
	through := model.TimeFromUnixNano(ts.Add(-c.cfg.MinCompactionAge).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod))
	return newDayRangeIterator(DayTable(from), DayTable(through))
}

func (c *Compactor) loadWork(ctx context.Context, ch chan<- tenantTable) error {
	tables := c.tables(time.Now())

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {

		table := tables.At()
		tablestr := fmt.Sprintf("%d", table)
		tenants, err := c.tenants(ctx, tablestr)
		if err != nil {
			return errors.Wrap(err, "getting tenants")
		}

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			tenant := tenants.At()
			ownershipRange, owns := c.ownsTenant(tenant)
			if !owns {
				continue
			}

			select {
			case ch <- tenantTable{tenant: tenant, table: table, ownershipRange: ownershipRange}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if err := tenants.Err(); err != nil {
			return errors.Wrap(err, "iterating tenants")
		}

	}

	if err := tables.Err(); err != nil {
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

				if err := c.compactTenantTable(ctx, tt); err != nil {
					return errors.Wrapf(
						err,
						"compacting tenant table (%s) for tenant (%s) with ownership (%s)",
						tt.table,
						tt.tenant,
						tt.ownershipRange,
					)
				}
			}
		}

	})

}

func (c *Compactor) compactTenantTable(ctx context.Context, tt tenantTable) error {
	level.Info(c.logger).Log("msg", "compacting", "org_id", tt.tenant, "table", tt.table, "ownership", tt.ownershipRange)
	return c.controller.buildBlocks(ctx, tt.table, tt.tenant, tt.ownershipRange)
}

type dayRangeIterator struct {
	min, max, cur DayTable
}

func newDayRangeIterator(min, max DayTable) *dayRangeIterator {
	return &dayRangeIterator{min: min, max: max, cur: min.Dec()}
}

func (r *dayRangeIterator) Next() bool {
	r.cur = r.cur.Inc()
	return r.cur.Before(r.max)
}

func (r *dayRangeIterator) At() DayTable {
	return r.cur
}

func (r *dayRangeIterator) Err() error {
	return nil
}
