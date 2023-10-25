package bloomgateway

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
)

// TODO(chaudum): Replace this placeholder with actual BlockRef struct.
type BlockRef struct {
	FromFp, ThroughFp uint64
	FromTs, ThroughTs int64
}

var (
	// BlocksOwnerSync is the operation used to check the authoritative owners of a block
	// (replicas included).
	BlocksOwnerSync = ring.NewOp([]ring.InstanceState{ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

	// BlocksOwnerRead is the operation used to check the authoritative owners of a block
	// (replicas included) that are available for queries (a bloom gateway is available for
	// queries only when ACTIVE).
	BlocksOwnerRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

	// BlocksRead is the operation run by the querier to query blocks via the bloom gateway.
	BlocksRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(s ring.InstanceState) bool {
		// Blocks can only be queried from ACTIVE instances. However, if the block belongs to
		// a non-active instance, then we should extend the replication set and try to query it
		// from the next ACTIVE instance in the ring (which is expected to have it because a
		// bloom gateway keeps their previously owned blocks until new owners are ACTIVE).
		return s != ring.ACTIVE
	})
)

type Limits interface {
	BloomGatewayShardSize(tenantID string) int
}

type ShardingStrategy interface {
	// FilterTenants whose indexes should be loaded by the index gateway.
	// Returns the list of user IDs that should be synced by the index gateway.
	FilterTenants(ctx context.Context, tenantIDs []string) ([]string, error)
	FilterBlocks(ctx context.Context, tenantID string, blockRefs []BlockRef) ([]BlockRef, error)
}

type ShuffleShardingStrategy struct {
	r            ring.ReadRing
	limits       Limits
	instanceAddr string
	instanceID   string
	logger       log.Logger
}

func NewShuffleShardingStrategy(r ring.ReadRing, l Limits, instanceAddr, instanceID string, logger log.Logger) *ShuffleShardingStrategy {
	return &ShuffleShardingStrategy{
		r:            r,
		limits:       l,
		instanceAddr: instanceAddr,
		instanceID:   instanceID,
		logger:       logger,
	}
}

// FilterTenants implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterTenants(_ context.Context, tenantIDs []string) ([]string, error) {
	// As a protection, ensure the bloom gateway instance is healthy in the ring. It could also be missing
	// in the ring if it was failing to heartbeat the ring and it got remove from another healthy bloom gateway
	// instance, because of the auto-forget feature.
	if set, err := s.r.GetAllHealthy(BlocksOwnerSync); err != nil {
		return nil, err
	} else if !set.Includes(s.instanceAddr) {
		return nil, errGatewayUnhealthy
	}

	var filteredIDs []string

	for _, tenantID := range tenantIDs {
		subRing := GetShuffleShardingSubring(s.r, tenantID, s.limits)

		// Include the user only if it belongs to this bloom gateway shard.
		if subRing.HasInstance(s.instanceID) {
			filteredIDs = append(filteredIDs, tenantID)
		}
	}

	return filteredIDs, nil
}

func getBucket(rangeMin, rangeMax, pos uint64) int {
	return 0
}

// FilterBlocks implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterBlocks(ctx context.Context, tenantID string, blockRefs []BlockRef) ([]BlockRef, error) {
	filteredBlockRefs := make([]BlockRef, 0, len(blockRefs))

	subRing := GetShuffleShardingSubring(s.r, tenantID, s.limits)

	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	var rs ring.ReplicationSet
	var err error

	for _, blockRef := range blockRefs {
		rs, err = subRing.Get(uint32(blockRef.FromFp), BlocksOwnerSync, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, err
		}
		// Include the block only if it belongs to this bloom gateway shard.
		if rs.Includes(s.instanceID) {
			filteredBlockRefs = append(filteredBlockRefs, blockRef)
			continue
		}

		rs, err = subRing.Get(uint32(blockRef.ThroughFp), BlocksOwnerSync, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, err
		}
		// Include the block only if it belongs to this bloom gateway shard.
		if rs.Includes(s.instanceID) {
			filteredBlockRefs = append(filteredBlockRefs, blockRef)
			continue
		}
	}
	return filteredBlockRefs, nil
}

// GetShuffleShardingSubring returns the subring to be used for a given user.
// This function should be used both by index gateway servers and clients in
// order to guarantee the same logic is used.
func GetShuffleShardingSubring(ring ring.ReadRing, tenantID string, limits Limits) ring.ReadRing {
	shardSize := limits.BloomGatewayShardSize(tenantID)

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
type NoopStrategy struct{}

func NewNoopStrategy() *NoopStrategy {
	return &NoopStrategy{}
}

// FilterTenants implements ShardingStrategy.
func (s *NoopStrategy) FilterTenants(ctx context.Context, tenantIDs []string) ([]string, error) {
	return tenantIDs, nil
}

// FilterBlocks implements ShardingStrategy.
func (s *NoopStrategy) FilterBlocks(ctx context.Context, tenantID string, blockRefs []BlockRef) ([]BlockRef, error) {
	return blockRefs, nil
}
