package indexgateway

import (
	"github.com/grafana/dskit/ring"
	"github.com/pkg/errors"
)

var (
	// IndexesSync is the operation used to check the authoritative owners of an index
	// (replicas included).
	IndexesSync = ring.NewOp([]ring.InstanceState{ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

	// IndexesRead is the operation run by the querier to query indexes via the index-gateway.
	IndexesRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(s ring.InstanceState) bool {
		// Indexes can only be queried from ACTIVE instances. However, if the index belongs to
		// a non-active instance, then we should extend the replication set and try to query it
		// from the next ACTIVE instance in the ring (which is expected to have it because an
		// index-gateway keeps their previously owned blocks until new owners are ACTIVE).
		return s != ring.ACTIVE
	})

	errGatewayUnhealthy = errors.New("index-gateway is unhealthy in the ring")
)

type Limits interface {
	IndexGatewayShardSize(tenantID string) int
}

type ShardingStrategy interface {
	// FilterUsers whose indexes should be loaded by the index gateway.
	// Returns the list of user IDs that should be synced by the index gateway.
	FilterUsers(tenantID []string) ([]string, error)
}

type ShuffleShardingStrategy struct {
	r            ring.ReadRing
	limits       Limits
	instanceAddr string
	instanceID   string
}

func NewShuffleShardingStrategy(r ring.ReadRing, l Limits, instanceAddr, instanceID string) *ShuffleShardingStrategy {
	return &ShuffleShardingStrategy{
		r:            r,
		limits:       l,
		instanceAddr: instanceAddr,
		instanceID:   instanceID,
	}
}

// FilterUsers implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterUsers(tenantIDs []string) ([]string, error) {
	// As a protection, ensure the store-gateway instance is healthy in the ring. It could also be missing
	// in the ring if it was failing to heartbeat the ring and it got remove from another healthy store-gateway
	// instance, because of the auto-forget feature.
	if set, err := s.r.GetAllHealthy(IndexesSync); err != nil {
		return nil, err
	} else if !set.Includes(s.instanceAddr) {
		return nil, errGatewayUnhealthy
	}

	var filteredIDs []string

	for _, tenantID := range tenantIDs {
		subRing := GetShuffleShardingSubring(s.r, tenantID, s.limits)

		// Include the user only if it belongs to this store-gateway shard.
		if subRing.HasInstance(s.instanceID) {
			filteredIDs = append(filteredIDs, tenantID)
		}
	}

	return filteredIDs, nil
}

// GetShuffleShardingSubring returns the subring to be used for a given user.
// This function should be used both by index gateway servers and clients in
// order to guarantee the same logic is used.
func GetShuffleShardingSubring(ring ring.ReadRing, tenantID string, limits Limits) ring.ReadRing {
	shardSize := limits.IndexGatewayShardSize(tenantID)

	// A shard size of 0 means shuffle sharding is disabled for this specific user,
	// so we just return the full ring so that indexes will be sharded across all index gateways.
	if shardSize <= 0 {
		return ring
	}

	return ring.ShuffleShard(tenantID, shardSize)
}

type MatchAllStrategy struct{}

func NewMatchAllStrategy() *MatchAllStrategy {
	return &MatchAllStrategy{}
}

// FilterUsers implements ShardingStrategy.
func (s *MatchAllStrategy) FilterUsers(tenantIDs []string) ([]string, error) {
	return tenantIDs, nil
}
