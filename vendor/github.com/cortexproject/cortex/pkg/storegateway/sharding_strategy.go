package storegateway

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"

	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
)

const (
	shardExcludedMeta = "shard-excluded"
)

type ShardingStrategy interface {
	// FilterUsers whose blocks should be loaded by the store-gateway. Returns the list of user IDs
	// that should be synced by the store-gateway.
	FilterUsers(ctx context.Context, userIDs []string) []string

	// FilterBlocks filters metas in-place keeping only blocks that should be loaded by the store-gateway.
	// The provided loaded map contains blocks which have been previously returned by this function and
	// are now loaded or loading in the store-gateway.
	FilterBlocks(ctx context.Context, userID string, metas map[ulid.ULID]*metadata.Meta, loaded map[ulid.ULID]struct{}, synced *extprom.TxGaugeVec) error
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

func (s *NoShardingStrategy) FilterBlocks(_ context.Context, _ string, _ map[ulid.ULID]*metadata.Meta, _ map[ulid.ULID]struct{}, _ *extprom.TxGaugeVec) error {
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
func (s *DefaultShardingStrategy) FilterBlocks(_ context.Context, _ string, metas map[ulid.ULID]*metadata.Meta, loaded map[ulid.ULID]struct{}, synced *extprom.TxGaugeVec) error {
	filterBlocksByRingSharding(s.r, s.instanceAddr, metas, loaded, synced, s.logger)
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
func (s *ShuffleShardingStrategy) FilterBlocks(_ context.Context, userID string, metas map[ulid.ULID]*metadata.Meta, loaded map[ulid.ULID]struct{}, synced *extprom.TxGaugeVec) error {
	subRing := GetShuffleShardingSubring(s.r, userID, s.limits)
	filterBlocksByRingSharding(subRing, s.instanceAddr, metas, loaded, synced, s.logger)
	return nil
}

func filterBlocksByRingSharding(r ring.ReadRing, instanceAddr string, metas map[ulid.ULID]*metadata.Meta, loaded map[ulid.ULID]struct{}, synced *extprom.TxGaugeVec, logger log.Logger) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

	for blockID := range metas {
		key := cortex_tsdb.HashBlockID(blockID)

		// Check if the block is owned by the store-gateway
		set, err := r.Get(key, BlocksOwnerSync, bufDescs, bufHosts, bufZones)

		// If an error occurs while checking the ring, we keep the previously loaded blocks.
		if err != nil {
			if _, ok := loaded[blockID]; ok {
				level.Warn(logger).Log("msg", "failed to check block owner but block is kept because was previously loaded", "block", blockID.String(), "err", err)
			} else {
				level.Warn(logger).Log("msg", "failed to check block owner and block has been excluded because was not previously loaded", "block", blockID.String(), "err", err)

				// Skip the block.
				synced.WithLabelValues(shardExcludedMeta).Inc()
				delete(metas, blockID)
			}

			continue
		}

		// Keep the block if it is owned by the store-gateway.
		if set.Includes(instanceAddr) {
			continue
		}

		// The block is not owned by the store-gateway. However, if it's currently loaded
		// we can safely unload it only once at least 1 authoritative owner is available
		// for queries.
		if _, ok := loaded[blockID]; ok {
			// The ring Get() returns an error if there's no available instance.
			if _, err := r.Get(key, BlocksOwnerRead, bufDescs, bufHosts, bufZones); err != nil {
				// Keep the block.
				continue
			}
		}

		// The block is not owned by the store-gateway and there's at least 1 available
		// authoritative owner available for queries, so we can filter it out (and unload
		// it if it was loaded).
		synced.WithLabelValues(shardExcludedMeta).Inc()
		delete(metas, blockID)
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

	// Keep track of the last blocks returned by the Filter() function.
	lastBlocks map[ulid.ULID]struct{}
}

func NewShardingMetadataFilterAdapter(userID string, strategy ShardingStrategy) block.MetadataFilter {
	return &shardingMetadataFilterAdapter{
		userID:     userID,
		strategy:   strategy,
		lastBlocks: map[ulid.ULID]struct{}{},
	}
}

// Filter implements block.MetadataFilter.
// This function is NOT safe for use by multiple goroutines concurrently.
func (a *shardingMetadataFilterAdapter) Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	if err := a.strategy.FilterBlocks(ctx, a.userID, metas, a.lastBlocks, synced); err != nil {
		return err
	}

	// Keep track of the last filtered blocks.
	a.lastBlocks = make(map[ulid.ULID]struct{}, len(metas))
	for blockID := range metas {
		a.lastBlocks[blockID] = struct{}{}
	}

	return nil
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
