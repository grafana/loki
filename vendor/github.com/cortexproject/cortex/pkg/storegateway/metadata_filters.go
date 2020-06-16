package storegateway

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"

	"github.com/cortexproject/cortex/pkg/ring"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
)

const (
	shardExcludedMeta = "shard-excluded"
)

// ShardingMetadataFilter represents struct that allows sharding using the ring.
// Not go-routine safe.
type ShardingMetadataFilter struct {
	r            *ring.Ring
	instanceAddr string
	logger       log.Logger
}

// NewShardingMetadataFilter creates ShardingMetadataFilter.
func NewShardingMetadataFilter(r *ring.Ring, instanceAddr string, logger log.Logger) *ShardingMetadataFilter {
	return &ShardingMetadataFilter{
		r:            r,
		instanceAddr: instanceAddr,
		logger:       logger,
	}
}

// Filter filters out blocks not included within the current shard.
func (f *ShardingMetadataFilter) Filter(_ context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	// Buffer internally used by the ring (give extra room for a JOINING + LEAVING instance).
	buf := make([]ring.IngesterDesc, 0, f.r.ReplicationFactor()+2)

	for blockID := range metas {
		key := cortex_tsdb.HashBlockID(blockID)
		set, err := f.r.Get(key, ring.BlocksSync, buf)

		// If there are no healthy instances in the replication set or
		// the replication set for this block doesn't include this instance
		// then we filter it out.
		if err != nil || !set.Includes(f.instanceAddr) {
			if err != nil {
				level.Warn(f.logger).Log("msg", "failed to get replication set for block", "block", blockID.String(), "err", err)
			}

			synced.WithLabelValues(shardExcludedMeta).Inc()
			delete(metas, blockID)
		}
	}

	return nil
}
