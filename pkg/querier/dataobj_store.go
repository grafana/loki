package querier

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/querier/dataobjread"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/deletion"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// DataObjStoreOption customizes the store returned by [NewDataObjStore].
type DataObjStoreOption func(*dataObjStore)

// WithDataObjMetadataCache serves each object's metadata region through cache, so opening an
// object does not read that region from object storage. A nil cache disables it.
func WithDataObjMetadataCache(cache dataobj.MetadataCache) DataObjStoreOption {
	return func(s *dataObjStore) { s.metadataCache = cache }
}

// WithDataObjHeadPrefetchBytes sets how many bytes an object open reads up front.
func WithDataObjHeadPrefetchBytes(bytes int64) DataObjStoreOption {
	return func(s *dataObjStore) { s.prefetchBytes = bytes }
}

// WithDataObjRangeConfig sets how byte-range reads under the dataset layer are parallelised and
// coalesced.
func WithDataObjRangeConfig(cfg rangeio.Config) DataObjStoreOption {
	return func(s *dataObjStore) { s.rangeConfig = cfg }
}

// WithDataObjStreamFilterer drops streams a request may not read.
func WithDataObjStreamFilterer(filterer chunk.RequestChunkFilterer) DataObjStoreOption {
	return func(s *dataObjStore) { s.filterer = filterer }
}

var _ Store = &dataObjStore{}

// dataObjStore serves stream-first metric queries from data objects and delegates every other
// [Store] method to the embedded chunk store, so it changes only how samples are read.
type dataObjStore struct {
	// Store is the chunk store.
	Store

	bucket    objstore.BucketReader
	metastore metastore.Metastore
	metrics   *dataobjread.Metrics

	// filterer rejects streams a request may not read. It is nil when nothing filters.
	filterer chunk.RequestChunkFilterer

	prefetchBytes int64
	rangeConfig   rangeio.Config
	metadataCache dataobj.MetadataCache
}

// NewDataObjStore returns a [Store] that serves stream-first metric queries from the data objects
// in bucket, resolving the sections to read through ms, and every other call through chunkStore.
//
// chunkStore, bucket and ms are required. Without them a query would dereference nil inside the
// planner's goroutine, taking the process down instead of failing the one query.
func NewDataObjStore(chunkStore Store, bucket objstore.BucketReader, ms metastore.Metastore, reg prometheus.Registerer, opts ...DataObjStoreOption) (Store, error) {
	if chunkStore == nil {
		return nil, errors.New("data object store: chunk store must not be nil")
	}
	if bucket == nil {
		return nil, errors.New("data object store: bucket must not be nil")
	}
	if ms == nil {
		return nil, errors.New("data object store: metastore must not be nil")
	}

	s := &dataObjStore{
		Store:         chunkStore,
		bucket:        bucket,
		metastore:     ms,
		metrics:       dataobjread.NewMetrics(reg),
		prefetchBytes: dataobjread.DefaultHeadPrefetchBytes,
		rangeConfig:   rangeio.DefaultConfig,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// String names the store in a trace.
func (s *dataObjStore) String() string { return "dataobj" }

// SelectSamples returns the samples of a metric query.
//
// A timestamp-first sample query goes to the chunk store too, because the samples read here
// carry no order at all.
//
// It decides nothing else about whether a query belongs here. Nothing in it verifies that the
// query's time range is one data objects cover, or that the tier is disjoint in time from the
// ingester's, which is what lets the samples go undeduplicated.
func (s *dataObjStore) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
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

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}
	// A literal or vector expression produces samples without reading logs, so its extractor is
	// nil and no section is worth touching. Check before SetupExtractor, which given deletes
	// wraps a nil extractor into a non-nil one and would stop this from firing.
	if extractor == nil {
		return iter.NoopSampleIterator, nil
	}
	extractor, err = deletion.SetupExtractor(req, extractor)
	if err != nil {
		return nil, err
	}

	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}
	matchers := dataObjStreamMatchers(selector.Matchers())
	if len(matchers) == 0 {
		// The metastore resolves nothing without a stream matcher, so an empty result would be
		// a silently wrong answer rather than an empty one. LogQL cannot produce such a
		// selector today, which is why this is an error and not a fallback.
		return nil, fmt.Errorf("data object metric queries need at least one stream matcher, query %q has none", req.Selector)
	}

	deletes, err := dataObjDeleteSelectors(req)
	if err != nil {
		return nil, err
	}
	projection, err := dataobjread.NewProjectionPlan(expr, deletes)
	if err != nil {
		return nil, err
	}

	assignment, err := dataObjRequestShard(req.Shards)
	if err != nil {
		return nil, err
	}
	query := dataobjread.QueryParams{
		Start:      req.Start,
		End:        req.End,
		Matchers:   matchers,
		Shard:      dataobjread.NewQueryShard(assignment),
		Projection: projection,
	}

	var filterer chunk.Filterer
	if s.filterer != nil {
		filterer = s.filterer.ForRequest(ctx)
	}

	// One capture spans the query, but only observations recorded into the logs.RegionRead
	// region below reach the query stats: ValueFromRegion rolls up that one name. The planner's
	// object opens and streams reads run outside any region and go unreported.
	ctx, _ = xcap.NewCapture(ctx, nil)
	ctx = rangeio.WithConfig(ctx, &s.rangeConfig)

	objects := dataobjread.NewOpenObjects(s.bucket, tenantID, s.prefetchBytes, s.metadataCache)
	tasks := dataobjread.NewPlanner(s.metastore, objects, filterer).Plan(ctx, query)

	// The dataset layer records its statistics into the region the context carries, and nothing
	// under logs.RowReader starts one, so without this the query's byte and row counts are lost.
	// The region name matches what the v2 engine reads, so both report the same statistics.
	readCtx, _ := xcap.StartRegion(ctx, logs.RegionRead)
	reader := dataobjread.NewLogReader(readCtx, objects, tasks, dataobjread.DefaultMaxConcurrency, dataobjread.DefaultReadBatchSize, s.metrics)

	// Resolving no section is not an error: the reader then yields no sample and a nil error.
	return dataobjread.NewSampleIterator(reader, extractor), nil
}

// dataObjStreamMatchers drops the synthetic matchers the metastore does not understand, leaving
// the tenant's stream-label selector.
func dataObjStreamMatchers(matchers []*labels.Matcher) []*labels.Matcher {
	out := make([]*labels.Matcher, 0, len(matchers))
	for _, matcher := range matchers {
		if matcher.Name == model.MetricNameLabel || matcher.Name == astmapper.ShardLabel {
			continue
		}
		out = append(out, matcher)
	}
	return out
}

// dataObjDeleteSelectors parses the selectors of the request's delete requests, so the projection
// can account for the columns their pipelines read.
//
// The selectors are parsed again inside deletion.SetupExtractor, which builds the filters. That
// is a few parses against a full section scan, and sharing them would mean a new accessor on
// the deletion package, so the duplicate parse stands.
func dataObjDeleteSelectors(req logql.SelectSampleParams) ([]syntax.LogSelectorExpr, error) {
	deletes := req.GetDeletes()
	if len(deletes) == 0 {
		return nil, nil
	}
	out := make([]syntax.LogSelectorExpr, 0, len(deletes))
	for _, del := range deletes {
		expr, err := syntax.ParseLogSelector(del.Selector, true)
		if err != nil {
			return nil, err
		}
		out = append(out, expr)
	}
	return out, nil
}

// dataObjRequestShard parses the query-frontend's shard assignment. It returns nil when the query
// is not sharded.
//
// More than one shard is an error. The chunk store reads the first and drops the rest, which
// under-counts without saying so; the ingester refuses instead, and so does this.
func dataObjRequestShard(shards []string) (*logql.Shard, error) {
	if len(shards) == 0 {
		return nil, nil
	}
	parsed, _, err := logql.ParseShards(shards)
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		// Reporting no shard would make every shard of the request read every stream, and the
		// frontend would then sum one whole result per shard.
		return nil, fmt.Errorf("data object metric queries could not read a shard from %v", shards)
	}
	if len(parsed) > 1 {
		return nil, fmt.Errorf("data object metric queries support one shard per request, got %d", len(parsed))
	}
	return parsed[0].Ptr(), nil
}
