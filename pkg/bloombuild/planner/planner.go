package planner

import (
	"context"
	"fmt"
	"github.com/grafana/loki/v3/pkg/storage"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

type Planner struct {
	services.Service

	cfg       Config
	limits    Limits
	schemaCfg config.SchemaConfig

	tsdbStore TSDBStore

	metrics *Metrics
	logger  log.Logger
}

func New(
	cfg Config,
	schemaCfg config.SchemaConfig,
	storeCfg storage.Config,
	storageMetrics storage.ClientMetrics,
	logger log.Logger,
	r prometheus.Registerer,
) (*Planner, error) {
	utillog.WarnExperimentalUse("Bloom Planner", logger)

	tsdbStore, err := NewTSDBStores(schemaCfg, storeCfg, storageMetrics, logger)
	if err != nil {
		return nil, fmt.Errorf("error creating TSDB store: %w", err)
	}

	p := &Planner{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		tsdbStore: tsdbStore,
		metrics:   NewMetrics(r),
		logger:    logger,
	}

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
		return err
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
			if err := p.runOne(ctx); err != nil {
				return err
			}
		}
	}
}

func (p *Planner) runOne(ctx context.Context) error {
	var (
		start  = time.Now()
		status = statusSuccess
	)
	defer func() {
		p.metrics.buildCompleted.WithLabelValues(status).Inc()
		p.metrics.buildTime.WithLabelValues(status).Observe(time.Since(start).Seconds())
	}()

	p.metrics.buildStarted.Inc()
	level.Info(p.logger).Log("msg", "running bloom build planning")

	tables := p.tables(time.Now())
	level.Debug(p.logger).Log("msg", "loaded tables", "tables", tables.TotalDays())

	work, err := p.loadWork(ctx, tables)
	if err != nil {
		status = statusFailure
		level.Error(p.logger).Log("msg", "error loading work", "err", err)
		return fmt.Errorf("error loading work: %w", err)
	}

	_ = work

	level.Info(p.logger).Log("msg", "bloom build iteration completed", "duration", time.Since(start).Seconds())
	return nil
}

func (p *Planner) tables(ts time.Time) *dayRangeIterator {
	// adjust the minimum by one to make it inclusive, which is more intuitive
	// for a configuration variable
	adjustedMin := min(p.cfg.MinTableOffset - 1)
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

			splitFactor := p.limits.BloomSplitSeriesKeyspaceByFactor(tenant)
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
