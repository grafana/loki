package indexgateway

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
)

func (cfg *PerIndexOwnershipConfig) registerReconcileFlags(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.RingCheckPeriod, prefix+"ring-check-period", 10*time.Second,
		"Experimental. How often the index gateway checks the ring for changes. When the ring changes, it loads the indexes it now owns and evicts the ones it no longer owns, instead of waiting for the next resync. 0 disables the check, so ownership changes are applied on resync only.")
}

// FilterTenantsToDrop returns the tenants whose index in table this instance
// no longer serves any shard of, so it can evict them. It matches
// downloads.TenantFilter. Files of shards no longer owned within a tenant
// that is still served are dropped by the index set's sync through
// FilterFiles.
//
// It returns no tenants unless this instance is ACTIVE in the ring: while it
// is JOINING (preloading), LEAVING (shutting down) or missing from the ring it
// keeps everything. A tenant is returned only if this instance is an owner of
// none of its shards, neither for routing (IndexOwnersRead) nor for loading
// (IndexesSync). IndexOwnersRead skips owners that are not ACTIVE yet and
// extends to the next instance, so while a new owner is JOINING and
// preloading, this instance still counts as an owner and keeps the index.
func (f *IndexOwnershipFilter) FilterTenantsToDrop(table string, tenantIDs []string) ([]string, error) {
	set, err := f.r.GetAllHealthy(IndexOwnersRead)
	if err != nil {
		return nil, err
	}
	if !set.Includes(f.instanceAddr) {
		return nil, nil
	}

	var drop []string
	for _, tenantID := range tenantIDs {
		k := f.shardFactor(tenantID)
		serving, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexOwnersRead)
		if err != nil {
			return nil, err
		}
		if len(serving) > 0 {
			continue
		}
		owned, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexesSync)
		if err != nil {
			return nil, err
		}
		if len(owned) == 0 {
			drop = append(drop, tenantID)
		}
	}
	return drop, nil
}

// IndexOwnershipWatcher reconciles the indexes an index gateway holds with the
// ones it owns whenever the ring changes, rather than waiting for the next
// resync. It does nothing until Activate is called, once the initial preload
// has run.
type IndexOwnershipWatcher struct {
	r         ring.ReadRing
	period    time.Duration
	preloader IndexPreloader
	logger    log.Logger

	active     chan struct{}
	activeOnce sync.Once
}

// NewIndexOwnershipWatcher returns a watcher that checks r every period and,
// when the instances, their tokens or their states have changed, calls
// preloader.PreloadIndexes, which loads newly owned indexes and evicts the
// ones no longer owned.
func NewIndexOwnershipWatcher(r ring.ReadRing, period time.Duration, preloader IndexPreloader, logger log.Logger) *IndexOwnershipWatcher {
	return &IndexOwnershipWatcher{
		r:         r,
		period:    period,
		preloader: preloader,
		logger:    logger,
		active:    make(chan struct{}),
	}
}

// Activate starts reconciling on ring changes. Before it is called, ring
// changes are ignored so that they cannot trigger query readiness before the
// initial preload.
func (w *IndexOwnershipWatcher) Activate() {
	w.activeOnce.Do(func() { close(w.active) })
}

// Run checks the ring until ctx is done. The first check after Activate
// always reconciles. A reconcile that fails is retried on the next check.
func (w *IndexOwnershipWatcher) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-w.active:
	}

	ticker := time.NewTicker(w.period)
	defer ticker.Stop()

	var last ring.ReplicationSet
	reconciled := false
	for {
		current, err := w.r.GetAllHealthy(IndexesSync)
		if err != nil {
			level.Warn(w.logger).Log("msg", "failed to read the index gateway ring to check for ownership changes", "err", err)
		} else if !reconciled || ring.HasReplicationSetChanged(last, current) {
			reason := "ring changed"
			if !reconciled {
				reason = "first check after preload"
			}
			level.Info(w.logger).Log("msg", "reconciling owned indexes", "reason", reason)
			start := time.Now()
			if err := w.preloader.PreloadIndexes(ctx); ctx.Err() != nil {
				return
			} else if err != nil {
				level.Error(w.logger).Log("msg", "failed to reconcile owned indexes, retrying on the next check", "duration", time.Since(start), "err", err)
			} else {
				level.Info(w.logger).Log("msg", "reconciled owned indexes", "duration", time.Since(start))
				last = current
				reconciled = true
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
