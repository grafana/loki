package bloomcompactor

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/pkg/errors"
)

type Router struct {
	interval   time.Duration // how often to run compaction loops
	controller *SimpleBloomController

	// we can parallelize by (tenant, table) tuples and we run `parallelism` workers
	parallelism int
	logger      log.Logger
}

type TenantTable struct {
	tenant, table string
}

func (r *Router) Tenants() (v1.Iterator[string], error) {
	return nil, nil
}

func (r *Router) Tables(tenant string) (v1.Iterator[string], error) {
	return nil, nil
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
	ch := make(chan TenantTable)
	wg.Add(1)
	go func() {
		workersErr = r.runWorkers(ctx, ch)
		wg.Done()
	}()

	err := r.loadWork(ctx, ch)

	wg.Wait()
	return multierror.New(workersErr, err, ctx.Err()).Err()
}

func (r *Router) loadWork(ctx context.Context, ch chan<- TenantTable) error {
	tenants, err := r.Tenants()
	if err != nil {
		return errors.Wrap(err, "getting tenants")
	}

	for tenants.Next() && ctx.Err() == nil && tenants.Err() == nil {
		tenant := tenants.At()
		tables, err := r.Tables(tenant)
		if err != nil {
			return errors.Wrap(err, "getting tables")
		}

		for tables.Next() && ctx.Err() == nil && tables.Err() == nil {
			table := tables.At()
			select {
			case ch <- TenantTable{tenant: tenant, table: table}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if err := tables.Err(); err != nil {
			return errors.Wrap(err, "iterating tables")
		}
	}

	if err := tenants.Err(); err != nil {
		return errors.Wrap(err, "iterating tenants")
	}

	close(ch)
	return ctx.Err()
}

func (r *Router) runWorkers(ctx context.Context, ch <-chan TenantTable) error {

	return concurrency.ForEachJob(ctx, r.parallelism, r.parallelism, func(ctx context.Context, idx int) error {

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case tt, ok := <-ch:
				if !ok {
					return nil
				}

				return r.compactTenantTable(ctx, tt.tenant, tt.table)
			}
		}

	})

}

func (r *Router) compactTenantTable(ctx context.Context, tenant, table string) error {
	level.Info(r.logger).Log("msg", "compacting", "tenant", tenant, "table", table)
	return nil
}
