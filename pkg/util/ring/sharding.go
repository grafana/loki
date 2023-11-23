package ring

import (
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
)

type TenantSharding interface {
	GetTenantSubRing(tenantID string) ring.ReadRing
	OwnsTenant(tenantID string) bool
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

func (s *TenantShuffleSharding) GetTenantSubRing(tenantID string) ring.ReadRing {
	shardSize := s.shardSizeForTenant(tenantID)

	// A shard size of 0 means shuffle sharding is disabled for this specific user,
	if shardSize <= 0 {
		return s.r
	}

	return s.r.ShuffleShard(tenantID, shardSize)
}

func (s *TenantShuffleSharding) OwnsTenant(tenantID string) bool {
	subRing := s.GetTenantSubRing(tenantID)
	return subRing.HasInstance(s.ringLifeCycler.GetInstanceID())
}

type FingerprintSharding interface {
	OwnsFingerprint(fp model.Fingerprint) (bool, error)
}

// FingerprintShuffleSharding is not thread-safe.
type FingerprintShuffleSharding struct {
	r              ring.ReadRing
	ringLifeCycler *ring.BasicLifecycler
	ringOp         ring.Operation

	// Buffers for ring.Get() calls.
	bufDescs           []ring.InstanceDesc
	bufHosts, bufZones []string
}

func NewFingerprintShuffleSharding(
	r ring.ReadRing,
	ringLifeCycler *ring.BasicLifecycler,
	ringOp ring.Operation,
) *FingerprintShuffleSharding {
	s := FingerprintShuffleSharding{
		r:              r,
		ringLifeCycler: ringLifeCycler,
		ringOp:         ringOp,
	}

	s.bufDescs, s.bufHosts, s.bufZones = ring.MakeBuffersForGet()

	return &s
}

func (s *FingerprintShuffleSharding) OwnsFingerprint(fp uint64) (bool, error) {
	rs, err := s.r.Get(uint32(fp), s.ringOp, s.bufDescs, s.bufHosts, s.bufZones)
	if err != nil {
		return false, err
	}

	return rs.Includes(s.ringLifeCycler.GetInstanceAddr()), nil
}

// NoopStrategy is an implementation of the ShardingStrategy that does not
// shard anything.
type NoopStrategy struct{}

// OwnsTenant implements TenantShuffleSharding.
func (s *NoopStrategy) OwnsTenant(_ string) bool {
	return false
}

// GetTenantSubRing implements TenantShuffleSharding.
func (s *NoopStrategy) GetTenantSubRing(_ string) ring.ReadRing {
	return nil
}

// OwnsFingerprint implements FingerprintSharding.
func (s *NoopStrategy) OwnsFingerprint(_ uint64) (bool, error) {
	return false, nil
}
