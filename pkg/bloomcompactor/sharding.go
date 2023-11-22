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
	OwnsJob(job Job) (bool, error)
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

// OwnsJob makes sure only a single compactor should execute the job.
func (s *ShuffleShardingStrategy) OwnsJob(job Job) (bool, error) {
	if !s.OwnsTenant(job.Tenant()) {
		return false, nil
	}

	tenantRing := s.GetTenantSubRing(job.Tenant())
	fpSharding := util_ring.NewFingerprintShuffleSharding(tenantRing, s.ringLifeCycler, RingOp)
	// TODO: A shard should either own all the fps of a job or not
	return fpSharding.OwnsFingerprint(uint64(0))
}

// NoopStrategy is an implementation of the ShardingStrategy that does not
// filter anything.
type NoopStrategy struct {
	util_ring.NoopStrategy
}

// OwnsJob implements TenantShuffleSharding.
func (s *NoopStrategy) OwnsJob(_ Job) (bool, error) {
	return true, nil
}

func NewNoopStrategy() *NoopStrategy {
	return &NoopStrategy{NoopStrategy: util_ring.NoopStrategy{}}
}
