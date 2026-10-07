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
)

// IndexOwnersRead is the operation used to find the instances that serve an
// index (tenant, table): ACTIVE instances only. Unlike IndexesRead, it extends
// the replica set past instances that are not ACTIVE, so while a new owner is
// still JOINING the instance it is replacing is still returned.
var IndexOwnersRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(s ring.InstanceState) bool {
	return s != ring.ACTIVE
})

// PerIndexOwnershipConfig configures per-index ownership on the index gateway:
// each (tenant, table) index is owned by replication-factor gateways, which
// preload it before they go ACTIVE in the ring.
type PerIndexOwnershipConfig struct {
	Enabled                  bool          `yaml:"enabled"`
	WaitStabilityMinDuration time.Duration `yaml:"wait_stability_min_duration"`
	WaitStabilityMaxDuration time.Duration `yaml:"wait_stability_max_duration"`
	PreloadTimeout           time.Duration `yaml:"preload_timeout"`
	RingCheckPeriod          time.Duration `yaml:"ring_check_period"`
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *PerIndexOwnershipConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false,
		"Experimental. Give each (tenant, table) index replication-factor owners in the index gateway ring. An index gateway preloads only the indexes it owns, after it joins the ring and before it becomes ACTIVE. Requires -index-gateway.mode=ring.")
	f.DurationVar(&cfg.WaitStabilityMinDuration, prefix+"wait-stability-min-duration", 0,
		"Experimental. Minimum time the ring must be stable before the index gateway preloads its owned indexes. Set it when many index gateways join at once, such as when the ring is created or scaled up, so that each one preloads what it owns once every new instance has registered its tokens. 0 disables the wait.")
	f.DurationVar(&cfg.WaitStabilityMaxDuration, prefix+"wait-stability-max-duration", 5*time.Minute,
		"Experimental. Maximum time to wait for the ring to be stable before preloading anyway. Only used when -index-gateway.per-index-ownership.wait-stability-min-duration is greater than 0.")
	f.DurationVar(&cfg.PreloadTimeout, prefix+"preload-timeout", 30*time.Minute,
		"Experimental. Maximum time to preload owned indexes before becoming ACTIVE anyway. Indexes not loaded by then are loaded by the periodic query readiness loop. 0 means no timeout.")
	cfg.registerReconcileFlags(prefix, f)
}

// Validate validates the config.
func (cfg *PerIndexOwnershipConfig) Validate(mode Mode) error {
	if !cfg.Enabled {
		return nil
	}
	if mode != RingMode {
		return errors.New("index-gateway.per-index-ownership.enabled requires index-gateway.mode=ring")
	}
	if cfg.WaitStabilityMinDuration < 0 || cfg.WaitStabilityMaxDuration < 0 || cfg.PreloadTimeout < 0 || cfg.RingCheckPeriod < 0 {
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
	return uint32(d.Sum64())
}

// IndexOwnership answers which index gateways own the index of a (tenant,
// table). Owners are found on the full ring, with the ring's replication
// factor. It is used by index gateways and by their clients.
type IndexOwnership struct {
	r ring.ReadRing
}

// NewIndexOwnership returns an IndexOwnership over r.
func NewIndexOwnership(r ring.ReadRing) *IndexOwnership {
	return &IndexOwnership{r: r}
}

// Owners returns the instances that own the index of tenant in table for op:
// IndexesSync for loading, IndexOwnersRead for routing and dropping.
func (o *IndexOwnership) Owners(tenant, table string, op ring.Operation) (ring.ReplicationSet, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	return o.r.Get(IndexOwnershipKey(tenant, table), op, bufDescs, bufHosts, bufZones)
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

// IndexOwnershipFilter keeps the tenants whose index in a table is owned by
// one index gateway instance, for loading (IndexesSync).
type IndexOwnershipFilter struct {
	ownership    *IndexOwnership
	r            ring.ReadRing
	instanceAddr string
}

// NewIndexOwnershipFilter returns a filter for the instance at instanceAddr.
func NewIndexOwnershipFilter(r ring.ReadRing, instanceAddr string) *IndexOwnershipFilter {
	return &IndexOwnershipFilter{ownership: NewIndexOwnership(r), r: r, instanceAddr: instanceAddr}
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

// IndexPreloader loads the indexes an index gateway owns.
type IndexPreloader interface {
	PreloadIndexes(ctx context.Context) error
}

// NewOwnedIndexPreload returns the function an index gateway runs after it is
// JOINING in the ring and before it becomes ACTIVE: wait for the ring to
// settle, then preload the indexes it owns. A preload that times out is logged
// and does not stop the instance from becoming ACTIVE; any other preload error
// is returned.
func NewOwnedIndexPreload(cfg PerIndexOwnershipConfig, r ring.ReadRing, preloader IndexPreloader, logger log.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		if cfg.WaitStabilityMinDuration > 0 {
			level.Info(logger).Log("msg", "waiting for the index gateway ring to be stable before preloading", "min_duration", cfg.WaitStabilityMinDuration, "max_duration", cfg.WaitStabilityMaxDuration)
			start := time.Now()
			if err := ring.WaitRingTokensStability(ctx, r, IndexesSync, cfg.WaitStabilityMinDuration, cfg.WaitStabilityMaxDuration); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				level.Warn(logger).Log("msg", "index gateway ring is not stable, preloading anyway", "waited", time.Since(start), "err", err)
			} else {
				level.Info(logger).Log("msg", "index gateway ring is stable", "waited", time.Since(start))
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
		err := preloader.PreloadIndexes(preloadCtx)
		switch {
		case err == nil:
			level.Info(logger).Log("msg", "preloaded owned indexes", "duration", time.Since(start))
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case preloadCtx.Err() != nil:
			level.Warn(logger).Log("msg", "preloading owned indexes timed out, becoming ACTIVE anyway", "duration", time.Since(start), "timeout", cfg.PreloadTimeout, "err", err)
			return nil
		default:
			return fmt.Errorf("preload owned indexes: %w", err)
		}
	}
}
