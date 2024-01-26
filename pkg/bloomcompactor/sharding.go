package bloomcompactor

import (
	"github.com/grafana/dskit/ring"

	util_ring "github.com/grafana/loki/pkg/util/ring"
)

var (
	// TODO: Should we include LEAVING instances in the replication set?
	RingOp = ring.NewOp([]ring.InstanceState{ring.JOINING, ring.ACTIVE}, nil)
)

// ShardingStrategy describes whether compactor "owns" given user or job.
type ShardingStrategy interface {
	util_ring.TenantSharding
	OwnsFingerprint(tenantID string, fp uint64) (bool, error)
}

type ShuffleShardingStrategy struct {
	util_ring.TenantSharding
	ringLifeCycler *ring.BasicLifecycler
}

func NewShuffleShardingStrategy(r *ring.Ring, ringLifecycler *ring.BasicLifecycler, limits Limits) *ShuffleShardingStrategy {
	s := ShuffleShardingStrategy{
		TenantSharding: util_ring.NewTenantShuffleSharding(r, ringLifecycler, limits.BloomCompactorShardSize),
		ringLifeCycler: ringLifecycler,
	}

	return &s
}

// OwnsFingerprint makes sure only a single compactor processes the fingerprint.
func (s *ShuffleShardingStrategy) OwnsFingerprint(tenantID string, fp uint64) (bool, error) {
	if !s.OwnsTenant(tenantID) {
		return false, nil
	}

	tenantRing := s.GetTenantSubRing(tenantID)
	fpSharding := util_ring.NewFingerprintShuffleSharding(tenantRing, s.ringLifeCycler, RingOp)
	return fpSharding.OwnsFingerprint(fp)
}

// NoopStrategy is an implementation of the ShardingStrategy that does not
// filter anything.
type NoopStrategy struct {
	util_ring.NoopStrategy
}

// OwnsFingerprint implements TenantShuffleSharding.
func (s *NoopStrategy) OwnsFingerprint(_ string, _ uint64) (bool, error) {
	return true, nil
}

func NewNoopStrategy() *NoopStrategy {
	return &NoopStrategy{NoopStrategy: util_ring.NoopStrategy{}}
}
