package indexgateway

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
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
		k := f.shardFactor(table, tenantID)
		ownedForRead, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexOwnersRead)
		if err != nil {
			return nil, err
		}
		ownedForSync, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexesSync)
		if err != nil {
			return nil, err
		}
		if len(ownedForRead) == 0 && len(ownedForSync) == 0 {
			drop = append(drop, tenantID)
		}
	}
	return drop, nil
}

// IndexOwnershipWatcher preloads the indexes an index gateway owns before it
// becomes ACTIVE, then, if RingCheckPeriod is set, reconciles the indexes it
// holds with the ones it owns whenever the ring changes, rather than waiting
// for the next resync.
type IndexOwnershipWatcher struct {
	cfg             PerIndexOwnershipConfig
	r               ring.ReadRing
	store           index.QueryReadinessEnsurer
	logger          log.Logger
	preloadDuration *prometheus.HistogramVec

	preloaded     chan struct{}
	preloadedOnce sync.Once
}

// NewIndexOwnershipWatcher returns a watcher that preloads owned indexes with
// store.EnsureQueryReadiness and, every cfg.RingCheckPeriod, checks r and calls
// it again when the instances, their tokens or their states have changed,
// which loads newly owned indexes and evicts the ones no longer owned.
func NewIndexOwnershipWatcher(cfg PerIndexOwnershipConfig, r ring.ReadRing, store index.QueryReadinessEnsurer, logger log.Logger, reg prometheus.Registerer) *IndexOwnershipWatcher {
	return &IndexOwnershipWatcher{
		cfg:    cfg,
		r:      r,
		store:  store,
		logger: logger,
		preloadDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "owned_index_preload_duration_seconds",
			Help:      "Time (in seconds) an index gateway spent in each phase of preloading the indexes it owns before becoming ACTIVE, by outcome. A wait_stability timeout means the ring was not stable and the preload went ahead anyway.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
		}, []string{"phase", "status"}),
		preloaded: make(chan struct{}),
	}
}

// Preload loads the indexes this gateway owns. The ring manager runs it while
// the gateway is JOINING, before it becomes ACTIVE. Run starts reconciling
// once it succeeds, so that ring changes cannot trigger query readiness
// before the initial preload.
func (w *IndexOwnershipWatcher) Preload(ctx context.Context) error {
	if err := w.preload(ctx); err != nil {
		return err
	}
	w.preloadedOnce.Do(func() { close(w.preloaded) })
	return nil
}

// Run checks the ring every RingCheckPeriod until ctx is done. The first check
// after Preload always reconciles. A reconcile that fails is retried on the
// next check. If RingCheckPeriod is 0, it only waits for ctx to be done:
// returning early would stop the module, and the store with it.
func (w *IndexOwnershipWatcher) Run(ctx context.Context) error {
	if w.cfg.RingCheckPeriod <= 0 {
		<-ctx.Done()
		return nil
	}

	select {
	case <-ctx.Done():
		return nil
	case <-w.preloaded:
	}

	ticker := time.NewTicker(w.cfg.RingCheckPeriod)
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
			if err := w.store.EnsureQueryReadiness(ctx); ctx.Err() != nil {
				return nil
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
			return nil
		case <-ticker.C:
		}
	}
}
