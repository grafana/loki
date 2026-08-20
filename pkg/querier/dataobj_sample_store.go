package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/dataobjmetrics"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/util/deletion"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// dataObjSampleStore is a querier.Store that serves SelectSamples (metric queries) from data objects.
// Every other Store method is delegated to the embedded chunk store, so this type is a drop-in
// replacement that only changes how samples are read.
//
// It reads data objects only in stream-first order and never deduplicates (Sample.Hash is 0): routing
// guarantees the data-object tier is disjoint in time from the ingester and chunk tiers, and data
// objects are internally deduplicated.
type dataObjSampleStore struct {
	Store // chunk store; delegate everything except SelectSamples

	bucket                   objstore.Bucket
	resolver                 dataObjSectionsResolver
	shardBucketFilterEnabled bool
	metadataCache            dataobj.MetadataCache
	logger                   log.Logger
	metrics                  *dataobjmetrics.Metrics
}

// DataObjSampleStoreOption customizes a dataObjSampleStore.
type DataObjSampleStoreOption func(*dataObjSampleStore)

// WithDataObjMetadataCache serves each object's metadata prefix through cache, avoiding a per-open
// object-storage read of the metadata. A nil cache disables it.
func WithDataObjMetadataCache(cache dataobj.MetadataCache) DataObjSampleStoreOption {
	return func(s *dataObjSampleStore) { s.metadataCache = cache }
}

// NewDataObjSampleStore returns a Store that serves stream-first metric queries from data objects in
// bucket, resolved via ms. Every other Store method — and SelectSamples for any non-stream-first order —
// is delegated to chunkStore. chunkStore may be nil to exercise the data-object read path in isolation
// (benchmarks, focused tests), in which case only stream-first SelectSamples works.
//
// When shardBucketFilterEnabled is true, a sharded query prunes streams by their __shard_bucket__ column
// before decoding labels, falling back to the fingerprint filter for objects that lack the column.
//
// When sectionsClient is non-nil, sections are resolved by the index-gateway (per 12h window, with the
// metastore as fallback) instead of locally; otherwise resolution goes straight to the metastore.
func NewDataObjSampleStore(chunkStore Store, bucket objstore.Bucket, ms metastore.Metastore, sectionsClient DataObjSectionsGatewayClient, shardBucketFilterEnabled bool, logger log.Logger, reg prometheus.Registerer, opts ...DataObjSampleStoreOption) Store {
	s := &dataObjSampleStore{
		Store:                    chunkStore,
		bucket:                   bucket,
		resolver:                 newDataObjSectionsResolver(ms, sectionsClient, reg, logger),
		shardBucketFilterEnabled: shardBucketFilterEnabled,
		logger:                   logger,
		metrics:                  dataobjmetrics.New(prometheus.WrapRegistererWithPrefix("loki_querier_", reg)),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *dataObjSampleStore) String() string { return "dataobj" }

func (s *dataObjSampleStore) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	// Data objects are read only in stream-first order; the emitted iterator relies on that. The
	// querier routes only stream-first queries here, but enforce it so any other caller stays correct
	// by falling back to the chunk store.
	if req.Order != logproto.SAMPLE_ORDER_BY_STREAM {
		return s.Store.SelectSamples(ctx, req)
	}

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}
	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}
	matchers := stripNonStreamMatchers(selector.Matchers())
	if len(matchers) == 0 {
		// The metastore requires at least one stream matcher to resolve sections.
		return iter.NoopSampleIterator, nil
	}
	shard, err := shardFromRequest(req.Shards)
	if err != nil {
		return nil, err
	}

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}
	extractor, err = deletion.SetupExtractor(req, extractor)
	if err != nil {
		return nil, err
	}

	cache := newDataObjCache(s.bucket, tenantID)
	cache.metadataCache = s.metadataCache

	// One capture spans the whole query, so every object-storage read is accounted and attributed to the
	// component that made it.
	captureCtx, _ := xcap.NewCapture(ctx, nil)
	tasks := newDataObjReadPlanner(s.resolver, cache, s.shardBucketFilterEnabled).plan(captureCtx, req.Start, req.End, matchers, shard, expr)

	logsCtx, _ := xcap.StartRegion(captureCtx, logs.RegionRead)
	reader := newDataObjLogReader(logsCtx, cache, tasks, defaultMaxConcurrency, defaultReadBatchSize, s.metrics)

	// dataObjAbortReader stops the background planner (and, through the reader's Close, releases the
	// cache) once reading finishes or on Close. A resolution error surfaces through the reader's Err;
	// no matching sections is not an error — the reader yields no samples with a nil Err.
	return newDataObjSampleIterator(newDataObjAbortReader(reader, tasks), extractor), nil
}

// stripNonStreamMatchers drops the synthetic matchers (__name__, __cortex_shard__) that the metastore
// does not understand, leaving only the tenant's stream-label selector.
func stripNonStreamMatchers(matchers []*labels.Matcher) []*labels.Matcher {
	out := make([]*labels.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m.Name == model.MetricNameLabel || m.Name == astmapper.ShardLabel {
			continue
		}
		out = append(out, m)
	}
	return out
}

// shardFromRequest parses the query-frontend shard assignment. Only the first shard is used, matching
// the chunk store's injectShardLabel. It returns nil when the query is not sharded.
func shardFromRequest(shards []string) (*logql.Shard, error) {
	if len(shards) == 0 {
		return nil, nil
	}
	parsed, _, err := logql.ParseShards(shards)
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	return parsed[0].Ptr(), nil
}
