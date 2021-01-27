package storegateway

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
)

// BucketIndexMetadataFetcher is a Thanos MetadataFetcher implementation leveraging on the Cortex bucket index.
type BucketIndexMetadataFetcher struct {
	userID    string
	bkt       objstore.Bucket
	strategy  ShardingStrategy
	logger    log.Logger
	filters   []block.MetadataFilter
	modifiers []block.MetadataModifier
	metrics   *fetcherMetrics
}

func NewBucketIndexMetadataFetcher(
	userID string,
	bkt objstore.Bucket,
	strategy ShardingStrategy,
	logger log.Logger,
	reg prometheus.Registerer,
	filters []block.MetadataFilter,
	modifiers []block.MetadataModifier,
) *BucketIndexMetadataFetcher {
	return &BucketIndexMetadataFetcher{
		userID:    userID,
		bkt:       bkt,
		strategy:  strategy,
		logger:    logger,
		filters:   filters,
		modifiers: modifiers,
		metrics:   newFetcherMetrics(reg),
	}
}

// Fetch implements metadata.MetadataFetcher.
func (f *BucketIndexMetadataFetcher) Fetch(ctx context.Context) (metas map[ulid.ULID]*metadata.Meta, partial map[ulid.ULID]error, err error) {
	f.metrics.resetTx()

	// Check whether the user belongs to the shard.
	if len(f.strategy.FilterUsers(ctx, []string{f.userID})) != 1 {
		f.metrics.submit()
		return nil, nil, nil
	}

	// Track duration and sync counters only if wasn't filtered out by the sharding strategy.
	start := time.Now()
	defer func() {
		f.metrics.syncDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			f.metrics.syncFailures.Inc()
		}
	}()
	f.metrics.syncs.Inc()

	// Fetch the bucket index.
	idx, err := bucketindex.ReadIndex(ctx, f.bkt, f.userID, f.logger)
	if errors.Is(err, bucketindex.ErrIndexNotFound) {
		// This is a legit case happening when the first blocks of a tenant have recently been uploaded by ingesters
		// and their bucket index has not been created yet.
		f.metrics.synced.WithLabelValues(noBucketIndex).Set(1)
		f.metrics.submit()

		return nil, nil, nil
	}
	if errors.Is(err, bucketindex.ErrIndexCorrupted) {
		// In case a single tenant bucket index is corrupted, we don't want the store-gateway to fail at startup
		// because unable to fetch blocks metadata. We'll act as if the tenant has no bucket index, but the query
		// will fail anyway in the querier (the querier fails in the querier if bucket index is corrupted).
		level.Error(f.logger).Log("msg", "corrupted bucket index found", "user", f.userID, "err", err)
		f.metrics.synced.WithLabelValues(corruptedBucketIndex).Set(1)
		f.metrics.submit()

		return nil, nil, nil
	}
	if err != nil {
		f.metrics.synced.WithLabelValues(failedMeta).Set(1)
		f.metrics.submit()

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
			err = customFilter.FilterWithBucketIndex(ctx, metas, idx, f.metrics.synced)
		} else {
			err = filter.Filter(ctx, metas, f.metrics.synced)
		}

		if err != nil {
			return nil, nil, errors.Wrap(err, "filter metas")
		}
	}

	for _, m := range f.modifiers {
		// NOTE: modifier can update modified metric accordingly to the reason of the modification.
		if err := m.Modify(ctx, metas, f.metrics.modified); err != nil {
			return nil, nil, errors.Wrap(err, "modify metas")
		}
	}

	f.metrics.synced.WithLabelValues(loadedMeta).Set(float64(len(metas)))
	f.metrics.submit()

	return metas, nil, nil
}

func (f *BucketIndexMetadataFetcher) UpdateOnChange(callback func([]metadata.Meta, error)) {
	// Unused by the store-gateway.
	callback(nil, errors.New("UpdateOnChange is unsupported"))
}

const (
	fetcherSubSys = "blocks_meta"

	corruptedMeta        = "corrupted-meta-json"
	noMeta               = "no-meta-json"
	loadedMeta           = "loaded"
	failedMeta           = "failed"
	corruptedBucketIndex = "corrupted-bucket-index"
	noBucketIndex        = "no-bucket-index"

	// Synced label values.
	labelExcludedMeta = "label-excluded"
	timeExcludedMeta  = "time-excluded"
	tooFreshMeta      = "too-fresh"
	duplicateMeta     = "duplicate"
	// Blocks that are marked for deletion can be loaded as well. This is done to make sure that we load blocks that are meant to be deleted,
	// but don't have a replacement block yet.
	markedForDeletionMeta = "marked-for-deletion"

	// MarkedForNoCompactionMeta is label for blocks which are loaded but also marked for no compaction. This label is also counted in `loaded` label metric.
	MarkedForNoCompactionMeta = "marked-for-no-compact"

	// Modified label values.
	replicaRemovedMeta = "replica-label-removed"
)

// fetcherMetrics is a copy of Thanos internal fetcherMetrics. These metrics have been copied from
// Thanos in order to track the same exact metrics in our own custom metadata fetcher implementation.
type fetcherMetrics struct {
	syncs        prometheus.Counter
	syncFailures prometheus.Counter
	syncDuration prometheus.Histogram

	synced   *extprom.TxGaugeVec
	modified *extprom.TxGaugeVec
}

func newFetcherMetrics(reg prometheus.Registerer) *fetcherMetrics {
	var m fetcherMetrics

	m.syncs = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Subsystem: fetcherSubSys,
		Name:      "syncs_total",
		Help:      "Total blocks metadata synchronization attempts",
	})
	m.syncFailures = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Subsystem: fetcherSubSys,
		Name:      "sync_failures_total",
		Help:      "Total blocks metadata synchronization failures",
	})
	m.syncDuration = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Subsystem: fetcherSubSys,
		Name:      "sync_duration_seconds",
		Help:      "Duration of the blocks metadata synchronization in seconds",
		Buckets:   []float64{0.01, 1, 10, 100, 1000},
	})
	m.synced = extprom.NewTxGaugeVec(
		reg,
		prometheus.GaugeOpts{
			Subsystem: fetcherSubSys,
			Name:      "synced",
			Help:      "Number of block metadata synced",
		},
		[]string{"state"},
		[]string{corruptedMeta},
		[]string{corruptedBucketIndex},
		[]string{noMeta},
		[]string{noBucketIndex},
		[]string{loadedMeta},
		[]string{tooFreshMeta},
		[]string{failedMeta},
		[]string{labelExcludedMeta},
		[]string{timeExcludedMeta},
		[]string{duplicateMeta},
		[]string{markedForDeletionMeta},
		[]string{MarkedForNoCompactionMeta},
	)
	m.modified = extprom.NewTxGaugeVec(
		reg,
		prometheus.GaugeOpts{
			Subsystem: fetcherSubSys,
			Name:      "modified",
			Help:      "Number of blocks whose metadata changed",
		},
		[]string{"modified"},
		[]string{replicaRemovedMeta},
	)
	return &m
}

func (s *fetcherMetrics) submit() {
	s.synced.Submit()
	s.modified.Submit()
}

func (s *fetcherMetrics) resetTx() {
	s.synced.ResetTx()
	s.modified.ResetTx()
}
