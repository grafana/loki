package storegateway

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/ring"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
)

const (
	shardExcludedMeta = "shard-excluded"
)

type ShardingStrategy interface {
	// FilterUsers whose blocks should be loaded by the store-gateway. Returns the list of user IDs
	// that should be synced by the store-gateway.
	FilterUsers(ctx context.Context, userIDs []string) []string

	// FilterBlocks that should be loaded by the store-gateway.
	FilterBlocks(ctx context.Context, userID string, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error
}

// ShardingLimits is the interface that should be implemented by the limits provider,
// limiting the scope of the limits to the ones required by sharding strategies.
type ShardingLimits interface {
	StoreGatewayTenantShardSize(userID string) int
}

// NoShardingStrategy is a no-op strategy. When this strategy is used, no tenant/block is filtered out.
type NoShardingStrategy struct{}

func NewNoShardingStrategy() *NoShardingStrategy {
	return &NoShardingStrategy{}
}

func (s *NoShardingStrategy) FilterUsers(_ context.Context, userIDs []string) []string {
	return userIDs
}

func (s *NoShardingStrategy) FilterBlocks(_ context.Context, _ string, _ map[ulid.ULID]*metadata.Meta, _ *extprom.TxGaugeVec) error {
	return nil
}

// DefaultShardingStrategy is a sharding strategy based on the hash ring formed by store-gateways.
// Not go-routine safe.
type DefaultShardingStrategy struct {
	r            *ring.Ring
	instanceAddr string
	logger       log.Logger
}

// NewDefaultShardingStrategy creates DefaultShardingStrategy.
func NewDefaultShardingStrategy(r *ring.Ring, instanceAddr string, logger log.Logger) *DefaultShardingStrategy {
	return &DefaultShardingStrategy{
		r:            r,
		instanceAddr: instanceAddr,
		logger:       logger,
	}
}

// FilterUsers implements ShardingStrategy.
func (s *DefaultShardingStrategy) FilterUsers(_ context.Context, userIDs []string) []string {
	return userIDs
}

// FilterBlocks implements ShardingStrategy.
func (s *DefaultShardingStrategy) FilterBlocks(_ context.Context, _ string, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	filterBlocksByRingSharding(s.r, s.instanceAddr, metas, synced, s.logger)
	return nil
}

// ShuffleShardingStrategy is a shuffle sharding strategy, based on the hash ring formed by store-gateways,
// where each tenant blocks are sharded across a subset of store-gateway instances.
type ShuffleShardingStrategy struct {
	r            *ring.Ring
	instanceID   string
	instanceAddr string
	limits       ShardingLimits
	logger       log.Logger
}

// NewShuffleShardingStrategy makes a new ShuffleShardingStrategy.
func NewShuffleShardingStrategy(r *ring.Ring, instanceID, instanceAddr string, limits ShardingLimits, logger log.Logger) *ShuffleShardingStrategy {
	return &ShuffleShardingStrategy{
		r:            r,
		instanceID:   instanceID,
		instanceAddr: instanceAddr,
		limits:       limits,
		logger:       logger,
	}
}

// FilterUsers implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterUsers(_ context.Context, userIDs []string) []string {
	var filteredIDs []string

	for _, userID := range userIDs {
		subRing := GetShuffleShardingSubring(s.r, userID, s.limits)

		// Include the user only if it belongs to this store-gateway shard.
		if subRing.HasInstance(s.instanceID) {
			filteredIDs = append(filteredIDs, userID)
		}
	}

	return filteredIDs
}

// FilterBlocks implements ShardingStrategy.
func (s *ShuffleShardingStrategy) FilterBlocks(_ context.Context, userID string, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	subRing := GetShuffleShardingSubring(s.r, userID, s.limits)
	filterBlocksByRingSharding(subRing, s.instanceAddr, metas, synced, s.logger)
	return nil
}

func filterBlocksByRingSharding(r ring.ReadRing, instanceAddr string, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec, logger log.Logger) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

	for blockID := range metas {
		key := cortex_tsdb.HashBlockID(blockID)
		set, err := r.Get(key, BlocksSync, bufDescs, bufHosts, bufZones)

		// If there are no healthy instances in the replication set or
		// the replication set for this block doesn't include this instance
		// then we filter it out.
		if err != nil || !set.Includes(instanceAddr) {
			if err != nil {
				level.Warn(logger).Log("msg", "excluded block because failed to get replication set", "block", blockID.String(), "err", err)
			}

			synced.WithLabelValues(shardExcludedMeta).Inc()
			delete(metas, blockID)
		}
	}
}

// GetShuffleShardingSubring returns the subring to be used for a given user. This function
// should be used both by store-gateway and querier in order to guarantee the same logic is used.
func GetShuffleShardingSubring(ring *ring.Ring, userID string, limits ShardingLimits) ring.ReadRing {
	shardSize := limits.StoreGatewayTenantShardSize(userID)

	// A shard size of 0 means shuffle sharding is disabled for this specific user,
	// so we just return the full ring so that blocks will be sharded across all store-gateways.
	if shardSize <= 0 {
		return ring
	}

	return ring.ShuffleShard(userID, shardSize)
}

type shardingMetadataFilterAdapter struct {
	userID   string
	strategy ShardingStrategy
}

func NewShardingMetadataFilterAdapter(userID string, strategy ShardingStrategy) block.MetadataFilter {
	return &shardingMetadataFilterAdapter{
		userID:   userID,
		strategy: strategy,
	}
}

// Filter implements block.MetadataFilter.
func (a *shardingMetadataFilterAdapter) Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	return a.strategy.FilterBlocks(ctx, a.userID, metas, synced)
}

type shardingBucketReaderAdapter struct {
	objstore.InstrumentedBucketReader

	userID   string
	strategy ShardingStrategy
}

func NewShardingBucketReaderAdapter(userID string, strategy ShardingStrategy, wrapped objstore.InstrumentedBucketReader) objstore.InstrumentedBucketReader {
	return &shardingBucketReaderAdapter{
		InstrumentedBucketReader: wrapped,
		userID:                   userID,
		strategy:                 strategy,
	}
}

// Iter implements objstore.BucketReader.
func (a *shardingBucketReaderAdapter) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	// Skip iterating the bucket if the tenant doesn't belong to the shard. From the caller
	// perspective, this will look like the tenant has no blocks in the storage.
	if len(a.strategy.FilterUsers(ctx, []string{a.userID})) == 0 {
		return nil
	}

	return a.InstrumentedBucketReader.Iter(ctx, dir, f, options...)
}
