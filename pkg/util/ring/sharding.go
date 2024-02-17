package ring

import (
	"github.com/grafana/dskit/ring"
)

type TenantSharding interface {
	OwnsTenant(tenantID string) (tenantRing ring.ReadRing, owned bool)
}

type TenantShuffleSharding struct {
	r                  ring.ReadRing
	ringLifeCycler     *ring.BasicLifecycler
	shardSizeForTenant func(tenantID string) int
}

func NewTenantShuffleSharding(
	r ring.ReadRing,
	ringLifeCycler *ring.BasicLifecycler,
	shardSizeForTenant func(tenantID string) int,
) *TenantShuffleSharding {
	return &TenantShuffleSharding{
		r:                  r,
		ringLifeCycler:     ringLifeCycler,
		shardSizeForTenant: shardSizeForTenant,
	}
}

func (s *TenantShuffleSharding) OwnsTenant(tenantID string) (ring.ReadRing, bool) {
	// A shard size of 0 means shuffle sharding is disabled for this specific user,
	shardSize := s.shardSizeForTenant(tenantID)
	if shardSize <= 0 {
		return s.r, true
	}

	subRing := s.r.ShuffleShard(tenantID, shardSize)
	if subRing.HasInstance(s.ringLifeCycler.GetInstanceID()) {
		return subRing, true
	}

	return nil, false
}

// NoopStrategy is an implementation of the ShardingStrategy that does not
// shard anything.
type NoopStrategy struct{}

// OwnsTenant implements TenantShuffleSharding.
func (s *NoopStrategy) OwnsTenant(_ string) (ring.ReadRing, bool) {
	return nil, false
}
