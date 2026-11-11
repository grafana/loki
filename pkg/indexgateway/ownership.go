package indexgateway

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// IndexOwnersRead is the operation used to find the instances that serve an
// index (tenant, table): ACTIVE instances only. Unlike IndexesRead, it extends
// the replica set past instances that are not ACTIVE, so while a new owner is
// still JOINING the instance it is replacing is still returned.
var IndexOwnersRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(s ring.InstanceState) bool {
	return s != ring.ACTIVE
})

// PerIndexOwnershipConfig configures per-index ownership on the index gateway:
// each (tenant, table) index is owned by ReplicationFactor gateways, which
// preload it before they go ACTIVE in the ring.
type PerIndexOwnershipConfig struct {
	Enabled                  bool          `yaml:"enabled" category:"experimental"`
	ReplicationFactor        int           `yaml:"replication_factor" category:"experimental"`
	WaitStabilityMinDuration time.Duration `yaml:"wait_stability_min_duration" category:"experimental"`
	WaitStabilityMaxDuration time.Duration `yaml:"wait_stability_max_duration" category:"experimental"`
	PreloadTimeout           time.Duration `yaml:"preload_timeout" category:"experimental"`
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *PerIndexOwnershipConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false,
		"Experimental. Give each (tenant, table) index -index-gateway.per-index-ownership.replication-factor owners in the index gateway ring. An index gateway preloads only the indexes it owns, after it joins the ring and before it becomes ACTIVE. Requires -index-gateway.mode=ring.")
	f.IntVar(&cfg.ReplicationFactor, prefix+"replication-factor", ReplicationFactor,
		"Experimental. Number of index gateways that own each (tenant, table) index. Index gateways and their clients must use the same value. It must be at least the replication factor of the index gateway ring, and a multiple of it when zone awareness is enabled.")
	f.DurationVar(&cfg.WaitStabilityMinDuration, prefix+"wait-stability-min-duration", 0,
		"Experimental. Minimum time the ring must be stable before the index gateway preloads its owned indexes. Set it when many index gateways join at once, such as when the ring is created or scaled up, so that each one preloads what it owns once every new instance has registered its tokens. 0 disables the wait.")
	f.DurationVar(&cfg.WaitStabilityMaxDuration, prefix+"wait-stability-max-duration", 5*time.Minute,
		"Experimental. Maximum time to wait for the ring to be stable before preloading anyway. Only used when -index-gateway.per-index-ownership.wait-stability-min-duration is greater than 0.")
	f.DurationVar(&cfg.PreloadTimeout, prefix+"preload-timeout", 30*time.Minute,
		"Experimental. Maximum time to preload owned indexes before becoming ACTIVE anyway. Indexes not loaded by then are loaded by the periodic query readiness loop. 0 means no timeout.")
}

// Validate validates the config against the mode and the ring config of the
// index gateway.
func (cfg *PerIndexOwnershipConfig) Validate(mode Mode, ringReplicationFactor int, zoneAwarenessEnabled bool) error {
	if !cfg.Enabled {
		return nil
	}
	if mode != RingMode {
		return errors.New("index-gateway.per-index-ownership.enabled requires index-gateway.mode=ring")
	}
	// The ring raises a smaller per-call replication factor to its own, and
	// only spreads a larger one evenly across zones if it is a multiple.
	if cfg.ReplicationFactor < ringReplicationFactor {
		return fmt.Errorf("index-gateway.per-index-ownership.replication-factor (%d) must be at least the index gateway ring replication factor (%d); lower the ring replication factor to use fewer owners", cfg.ReplicationFactor, ringReplicationFactor)
	}
	if zoneAwarenessEnabled && cfg.ReplicationFactor%ringReplicationFactor != 0 {
		return fmt.Errorf("index-gateway.per-index-ownership.replication-factor (%d) must be a multiple of the index gateway ring replication factor (%d) when zone awareness is enabled", cfg.ReplicationFactor, ringReplicationFactor)
	}
	if cfg.WaitStabilityMinDuration < 0 || cfg.WaitStabilityMaxDuration < 0 || cfg.PreloadTimeout < 0 {
		return errors.New("index-gateway.per-index-ownership durations must not be negative")
	}
	if cfg.WaitStabilityMinDuration > 0 && cfg.WaitStabilityMaxDuration < cfg.WaitStabilityMinDuration {
		return errors.New("index-gateway.per-index-ownership.wait-stability-max-duration must be at least wait-stability-min-duration")
	}
	return nil
}

// IndexOwnershipKey returns the ring key of the index of tenant in table, where
// table is the full table name (e.g. "index_19500"). Index gateways and their
// clients must use the same key.
func IndexOwnershipKey(tenant, table string) uint32 {
	d := xxhash.New()
	_, _ = d.WriteString(tenant)
	_, _ = d.WriteString("/")
	_, _ = d.WriteString(table)
	_, _ = d.WriteString("/0_of_1")
	return uint32(d.Sum64())
}

// IndexOwnership answers which index gateways own the index of a (tenant,
// table). Owners are found on the full ring, replicationFactor of them per
// index. It is used by index gateways and by their clients.
type IndexOwnership struct {
	r                 ring.ReadRing
	replicationFactor int
}

// NewIndexOwnership returns an IndexOwnership over r.
func NewIndexOwnership(r ring.ReadRing, replicationFactor int) *IndexOwnership {
	return &IndexOwnership{r: r, replicationFactor: replicationFactor}
}

// Owners returns the instances that own the index of tenant in table for op:
// IndexesSync for loading, IndexOwnersRead for routing and dropping.
func (o *IndexOwnership) Owners(tenant, table string, op ring.Operation) (ring.ReplicationSet, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	return o.r.GetWithOptions(IndexOwnershipKey(tenant, table), op,
		ring.WithBuffers(bufDescs, bufHosts, bufZones),
		ring.WithReplicationFactor(o.replicationFactor),
	)
}

// Owns reports whether the instance at instanceAddr owns the index of tenant
// in table for op.
func (o *IndexOwnership) Owns(instanceAddr, tenant, table string, op ring.Operation) (bool, error) {
	rs, err := o.Owners(tenant, table, op)
	if err != nil {
		return false, err
	}
	return rs.Includes(instanceAddr), nil
}

// IndexOwnershipFilter selects the tenants whose index for a time period
// (table) is owned by the gateway at instanceAddr.
type IndexOwnershipFilter struct {
	ownership    *IndexOwnership
	r            ring.ReadRing
	instanceAddr string
}

// NewIndexOwnershipFilter returns a filter for the instance at instanceAddr.
func NewIndexOwnershipFilter(r ring.ReadRing, replicationFactor int, instanceAddr string) *IndexOwnershipFilter {
	return &IndexOwnershipFilter{ownership: NewIndexOwnership(r, replicationFactor), r: r, instanceAddr: instanceAddr}
}

// FilterTenants returns the tenants whose index in table this instance owns.
// It matches downloads.TenantFilter.
func (f *IndexOwnershipFilter) FilterTenants(table string, tenantIDs []string) ([]string, error) {
	// As in ShuffleShardingStrategy, make sure this instance is in the ring:
	// one that has dropped out would otherwise conclude it owns nothing.
	if set, err := f.r.GetAllHealthy(IndexesSync); err != nil {
		return nil, err
	} else if !set.Includes(f.instanceAddr) {
		return nil, errGatewayUnhealthy
	}

	var owned []string
	for _, tenantID := range tenantIDs {
		ok, err := f.ownership.Owns(f.instanceAddr, tenantID, table, IndexesSync)
		if err != nil {
			return nil, err
		}
		if ok {
			owned = append(owned, tenantID)
		}
	}
	return owned, nil
}

// Phases and statuses of the owned index preload, as observed by
// loki_index_gateway_owned_index_preload_duration_seconds.
const (
	preloadPhaseWaitStability = "wait_stability"
	preloadPhasePreload       = "preload"

	preloadStatusSuccess  = "success"
	preloadStatusTimeout  = "timeout"
	preloadStatusError    = "error"
	preloadStatusCanceled = "canceled"
)


// NewOwnedIndexPreload returns the function an ingex gateway runs while it is
// JOINING the ring, before it becomes ACTIVE. This funciton is used to preload
// data. If the ring does not settle before the preoload timeout, it logs and
// returns nil so the gateway still becomes ACTIVE. Any other error fails
// startup.
func NewOwnedIndexPreload(cfg PerIndexOwnershipConfig, r ring.ReadRing, store index.QueryReadinessEnsurer, logger log.Logger, reg prometheus.Registerer) func(context.Context) error {
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Subsystem: "index_gateway",
		Name:      "owned_index_preload_duration_seconds",
		Help:      "Time (in seconds) an index gateway spent in each phase of preloading the indexes it owns before becoming ACTIVE, by outcome. A wait_stability timeout means the ring was not stable and the preload went ahead anyway.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 13),
	}, []string{"phase", "status"})

	return func(ctx context.Context) error {
		if cfg.WaitStabilityMinDuration > 0 {
			level.Info(logger).Log("msg", "waiting for the index gateway ring to be stable before preloading", "min_duration", cfg.WaitStabilityMinDuration, "max_duration", cfg.WaitStabilityMaxDuration)
			start := time.Now()
			err := ring.WaitRingTokensStability(ctx, r, IndexesSync, cfg.WaitStabilityMinDuration, cfg.WaitStabilityMaxDuration)
			switch {
			case err == nil:
				duration.WithLabelValues(preloadPhaseWaitStability, preloadStatusSuccess).Observe(time.Since(start).Seconds())
				level.Info(logger).Log("msg", "index gateway ring is stable", "waited", time.Since(start))
			case ctx.Err() != nil:
				duration.WithLabelValues(preloadPhaseWaitStability, preloadStatusCanceled).Observe(time.Since(start).Seconds())
				return ctx.Err()
			default:
				duration.WithLabelValues(preloadPhaseWaitStability, preloadStatusTimeout).Observe(time.Since(start).Seconds())
				level.Warn(logger).Log("msg", "index gateway ring is not stable, preloading anyway", "waited", time.Since(start), "err", err)
			}
		}

		preloadCtx := ctx
		if cfg.PreloadTimeout > 0 {
			var cancel context.CancelFunc
			preloadCtx, cancel = context.WithTimeout(ctx, cfg.PreloadTimeout)
			defer cancel()
		}

		level.Info(logger).Log("msg", "preloading owned indexes")
		start := time.Now()
		err := store.EnsureQueryReadiness(preloadCtx)
		switch {
		case err == nil:
			duration.WithLabelValues(preloadPhasePreload, preloadStatusSuccess).Observe(time.Since(start).Seconds())
			level.Info(logger).Log("msg", "preloaded owned indexes", "duration", time.Since(start))
			return nil
		case ctx.Err() != nil:
			duration.WithLabelValues(preloadPhasePreload, preloadStatusCanceled).Observe(time.Since(start).Seconds())
			return ctx.Err()
		case preloadCtx.Err() != nil:
			duration.WithLabelValues(preloadPhasePreload, preloadStatusTimeout).Observe(time.Since(start).Seconds())
			level.Warn(logger).Log("msg", "preloading owned indexes timed out, becoming ACTIVE anyway", "duration", time.Since(start), "timeout", cfg.PreloadTimeout, "err", err)
			return nil
		default:
			duration.WithLabelValues(preloadPhasePreload, preloadStatusError).Observe(time.Since(start).Seconds())
			return fmt.Errorf("preload owned indexes: %w", err)
		}
	}
}
