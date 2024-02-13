package bloomcompactor

import (
	"context"
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

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
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

	sharding ShardingStrategy

	metrics   *Metrics
	btMetrics *v1.Metrics
}

func New(
	cfg Config,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	clientMetrics storage.ClientMetrics,
	fetcherProvider stores.ChunkFetcherProvider,
	sharding ShardingStrategy,
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
	c.btMetrics = v1.NewMetrics(prometheus.WrapRegistererWithPrefix("loki_bloom_tokenizer", r))
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

	c.metrics.compactionRunInterval.Set(cfg.CompactionInterval.Seconds())
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

		case <-ticker.C:
			if err := c.runOne(ctx); err != nil {
				level.Error(c.logger).Log("msg", "compaction iteration failed", "err", err)
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

type tenantTable struct {
	tenant         string
	table          DayTable
	ownershipRange v1.FingerprintBounds
}

func (c *Compactor) tenants(ctx context.Context, table DayTable) (v1.Iterator[string], error) {
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
	// adjust the minimum by one to make it inclusive, which is more intuitive
	// for a configuration variable
	adjustedMin := min(c.cfg.MinTableCompactionPeriod - 1)
	minCompactionPeriod := time.Duration(adjustedMin) * config.ObjectStorageIndexRequiredPeriod
	maxCompactionPeriod := time.Duration(c.cfg.MaxTableCompactionPeriod) * config.ObjectStorageIndexRequiredPeriod

	from := ts.Add(-maxCompactionPeriod).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)
	through := ts.Add(-minCompactionPeriod).UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod) * int64(config.ObjectStorageIndexRequiredPeriod)

	fromDay := DayTable(model.TimeFromUnixNano(from))
	throughDay := DayTable(model.TimeFromUnixNano(through))
	return newDayRangeIterator(fromDay, throughDay)

}

func (c *Compactor) loadWork(ctx context.Context, ch chan<- tenantTable) error {
	tables := c.tables(time.Now())

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {

		table := tables.At()
		tenants, err := c.tenants(ctx, table)
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
