package bloomcompactor

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type Router struct {
	// TODO(owen-d): configure these w/ limits
	interval           time.Duration // how often to run compaction loops
	minTable, maxTable int

	controller *SimpleBloomController
	tsdbStore  TSDBStore

	// we can parallelize by (tenant, table) tuples and we run `parallelism` workers
	parallelism int
	logger      log.Logger
}

type tenantTable struct {
	tenant, table  string
	ownershipRange v1.FingerprintBounds
}

func (r *Router) Tenants(ctx context.Context, table string) (v1.Iterator[string], error) {
	tenants, err := r.tsdbStore.UsersForPeriod(ctx, table)
	if err != nil {
		return nil, errors.Wrap(err, "getting tenants")
	}

	return v1.NewSliceIter(tenants), nil
}

// TODO(owen-d): implement w/ subrings
func (r *Router) ownsTenant(tenant string) (ownershipRange v1.FingerprintBounds, owns bool) {
	return v1.NewBounds(0, math.MaxUint64), true
}

// TODO(owen-d): parameterize via limits
func (r *Router) Tables() (v1.Iterator[int], error) {
	return newRangeIterator(r.minTable, r.maxTable), nil
}

func (r *Router) run(ctx context.Context) error {
	// run once at beginning
	if err := r.runOne(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(r.interval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := r.runOne(ctx); err != nil {
				level.Error(r.logger).Log("msg", "compaction iteration failed", "err", err)
				return err
			}
		}
	}
}

// runs a single round of compaction for all relevant tenants and tables
func (r *Router) runOne(ctx context.Context) error {
	var workersErr error
	var wg sync.WaitGroup
	ch := make(chan tenantTable)
	wg.Add(1)
	go func() {
		workersErr = r.runWorkers(ctx, ch)
		wg.Done()
	}()

	err := r.loadWork(ctx, ch)

	wg.Wait()
	return multierror.New(workersErr, err, ctx.Err()).Err()
}

func (r *Router) loadWork(ctx context.Context, ch chan<- tenantTable) error {
	tables, err := r.Tables()
	if err != nil {
		return errors.Wrap(err, "getting tables")
	}

	for tables.Next() && tables.Err() == nil && ctx.Err() == nil {

		table := tables.At()
		tablestr := fmt.Sprintf("%d", table)
		tenants, err := r.Tenants(ctx, tablestr)
		if err != nil {
			return errors.Wrap(err, "getting tenants")
		}

		for tenants.Next() && tenants.Err() == nil && ctx.Err() == nil {
			tenant := tenants.At()
			ownershipRange, owns := r.ownsTenant(tenant)
			if !owns {
				continue
			}

			select {
			case ch <- tenantTable{tenant: tenant, table: tablestr, ownershipRange: ownershipRange}:
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

func (r *Router) runWorkers(ctx context.Context, ch <-chan tenantTable) error {

	return concurrency.ForEachJob(ctx, r.parallelism, r.parallelism, func(ctx context.Context, idx int) error {

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case tt, ok := <-ch:
				if !ok {
					return nil
				}

				if err := r.compactTenantTable(ctx, tt); err != nil {
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

func (r *Router) compactTenantTable(ctx context.Context, tt tenantTable) error {
	level.Info(r.logger).Log("msg", "compacting", "org_id", tt.tenant, "table", tt.table, "ownership", tt.ownershipRange)
	return r.controller.buildBlocks(ctx, tt.table, tt.tenant, tt.ownershipRange)
}

type rangeIterator struct {
	min, max, cur int
}

func newRangeIterator(min, max int) *rangeIterator {
	return &rangeIterator{min: min, max: max, cur: min - 1}
}

func (r *rangeIterator) Next() bool {
	r.cur++
	return r.cur < r.max
}

func (r *rangeIterator) At() int {
	return r.cur
}

func (r *rangeIterator) Err() error {
	return nil
}
