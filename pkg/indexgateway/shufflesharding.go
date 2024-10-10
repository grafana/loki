package indexgateway

import (
	"github.com/grafana/dskit/ring"
	"github.com/pkg/errors"

	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

var (
	// IndexesSync is the operation used to check the authoritative owners of an index
	// (replicas included).
	IndexesSync = ring.NewOp([]ring.InstanceState{ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

	// IndexesRead is the operation run by the querier/query frontent to query
	// indexes via the index gateway.
	IndexesRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

	errGatewayUnhealthy = errors.New("index-gateway is unhealthy in the ring")
)

type Limits interface {
	IndexGatewayShardSize(tenantID string) int
	TSDBMaxBytesPerShard(string) int
	TSDBPrecomputeChunks(string) bool
}

type ShardingStrategy interface {
	// FilterTenants whose indexes should be loaded by the index gateway.
	// Returns the list of user IDs that should be synced by the index gateway.
	FilterTenants(tenantID []string) ([]string, error)
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

// FilterTenants implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterTenants(tenantIDs []string) ([]string, error) {
	// As a protection, ensure the index-gateway instance is healthy in the ring. It could also be missing
	// in the ring if it was failing to heartbeat the ring and it got remove from another healthy index-gateway
	// instance, because of the auto-forget feature.
	if set, err := s.r.GetAllHealthy(IndexesSync); err != nil {
		return nil, err
	} else if !set.Includes(s.instanceAddr) {
		return nil, errGatewayUnhealthy
	}

	var filteredIDs []string

	for _, tenantID := range tenantIDs {
		subRing := GetShuffleShardingSubring(s.r, tenantID, s.limits)

		// Include the user only if it belongs to this index-gateway shard.
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
	// Since we set the shard size to replication factor if shard size is 0, this
	// can only happen if both the shard size and the replication factor are set
	// to 0.
	if shardSize <= 0 {
		return ring
	}

	return ring.ShuffleShard(tenantID, shardSize)
}

// NoopStrategy is an implementation of the ShardingStrategy that does not
// filter anything.
// This is used when the index gateway runs in simple mode or when the index
// gateway runs in ring mode, but the ring manager runs in client mode.
type NoopStrategy struct{}

func NewNoopStrategy() *NoopStrategy {
	return &NoopStrategy{}
}

// FilterTenants implements ShardingStrategy.
func (s *NoopStrategy) FilterTenants(tenantIDs []string) ([]string, error) {
	return tenantIDs, nil
}

// GetShardingStrategy returns the correct ShardingStrategy implementation based
// on provided configuration.
func GetShardingStrategy(cfg Config, indexGatewayRingManager *lokiring.RingManager, o Limits) ShardingStrategy {
	if cfg.Mode != RingMode || indexGatewayRingManager.Mode == lokiring.ClientMode {
		return NewNoopStrategy()
	}
	instanceAddr := indexGatewayRingManager.RingLifecycler.GetInstanceAddr()
	instanceID := indexGatewayRingManager.RingLifecycler.GetInstanceID()
	return NewShuffleShardingStrategy(indexGatewayRingManager.Ring, o, instanceAddr, instanceID)
}
