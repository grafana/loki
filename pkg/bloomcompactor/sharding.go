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
	return fpSharding.OwnsFingerprint(uint64(job.Fingerprint()))
}
