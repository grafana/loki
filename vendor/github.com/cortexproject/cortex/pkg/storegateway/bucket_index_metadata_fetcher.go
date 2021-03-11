package storegateway

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
)

const (
	corruptedBucketIndex = "corrupted-bucket-index"
	noBucketIndex        = "no-bucket-index"
)

// BucketIndexMetadataFetcher is a Thanos MetadataFetcher implementation leveraging on the Cortex bucket index.
type BucketIndexMetadataFetcher struct {
	userID      string
	bkt         objstore.Bucket
	strategy    ShardingStrategy
	cfgProvider bucket.TenantConfigProvider
	logger      log.Logger
	filters     []block.MetadataFilter
	modifiers   []block.MetadataModifier
	metrics     *block.FetcherMetrics
}

func NewBucketIndexMetadataFetcher(
	userID string,
	bkt objstore.Bucket,
	strategy ShardingStrategy,
	cfgProvider bucket.TenantConfigProvider,
	logger log.Logger,
	reg prometheus.Registerer,
	filters []block.MetadataFilter,
	modifiers []block.MetadataModifier,
) *BucketIndexMetadataFetcher {
	return &BucketIndexMetadataFetcher{
		userID:      userID,
		bkt:         bkt,
		strategy:    strategy,
		cfgProvider: cfgProvider,
		logger:      logger,
		filters:     filters,
		modifiers:   modifiers,
		metrics:     block.NewFetcherMetrics(reg, [][]string{{corruptedBucketIndex}, {noBucketIndex}}, nil),
	}
}

// Fetch implements block.MetadataFetcher. Not goroutine-safe.
func (f *BucketIndexMetadataFetcher) Fetch(ctx context.Context) (metas map[ulid.ULID]*metadata.Meta, partial map[ulid.ULID]error, err error) {
	f.metrics.ResetTx()

	// Check whether the user belongs to the shard.
	if len(f.strategy.FilterUsers(ctx, []string{f.userID})) != 1 {
		f.metrics.Submit()
		return nil, nil, nil
	}

	// Track duration and sync counters only if wasn't filtered out by the sharding strategy.
	start := time.Now()
	defer func() {
		f.metrics.SyncDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			f.metrics.SyncFailures.Inc()
		}
	}()
	f.metrics.Syncs.Inc()

	// Fetch the bucket index.
	idx, err := bucketindex.ReadIndex(ctx, f.bkt, f.userID, f.cfgProvider, f.logger)
	if errors.Is(err, bucketindex.ErrIndexNotFound) {
		// This is a legit case happening when the first blocks of a tenant have recently been uploaded by ingesters
		// and their bucket index has not been created yet.
		f.metrics.Synced.WithLabelValues(noBucketIndex).Set(1)
		f.metrics.Submit()

		return nil, nil, nil
	}
	if errors.Is(err, bucketindex.ErrIndexCorrupted) {
		// In case a single tenant bucket index is corrupted, we don't want the store-gateway to fail at startup
		// because unable to fetch blocks metadata. We'll act as if the tenant has no bucket index, but the query
		// will fail anyway in the querier (the querier fails in the querier if bucket index is corrupted).
		level.Error(f.logger).Log("msg", "corrupted bucket index found", "user", f.userID, "err", err)
		f.metrics.Synced.WithLabelValues(corruptedBucketIndex).Set(1)
		f.metrics.Submit()

		return nil, nil, nil
	}
	if err != nil {
		f.metrics.Synced.WithLabelValues(block.FailedMeta).Set(1)
		f.metrics.Submit()

		return nil, nil, errors.Wrapf(err, "read bucket index")
	}

	// Build block metas out of the index.
	metas = make(map[ulid.ULID]*metadata.Meta, len(idx.Blocks))
	for _, b := range idx.Blocks {
		metas[b.ID] = b.ThanosMeta(f.userID)
	}

	for _, filter := range f.filters {
		var err error

		// NOTE: filter can update synced metric accordingly to the reason of the exclude.
		if customFilter, ok := filter.(MetadataFilterWithBucketIndex); ok {
			err = customFilter.FilterWithBucketIndex(ctx, metas, idx, f.metrics.Synced)
		} else {
			err = filter.Filter(ctx, metas, f.metrics.Synced)
		}

		if err != nil {
			return nil, nil, errors.Wrap(err, "filter metas")
		}
	}

	for _, m := range f.modifiers {
		// NOTE: modifier can update modified metric accordingly to the reason of the modification.
		if err := m.Modify(ctx, metas, f.metrics.Modified); err != nil {
			return nil, nil, errors.Wrap(err, "modify metas")
		}
	}

	f.metrics.Synced.WithLabelValues(block.LoadedMeta).Set(float64(len(metas)))
	f.metrics.Submit()

	return metas, nil, nil
}

func (f *BucketIndexMetadataFetcher) UpdateOnChange(callback func([]metadata.Meta, error)) {
	// Unused by the store-gateway.
	callback(nil, errors.New("UpdateOnChange is unsupported"))
}
