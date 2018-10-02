package chunk

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/weaveworks/common/user"
)

var (
	errCardinalityExceeded = errors.New("cardinality limit exceeded")

	indexLookupsPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_index_lookups_per_query",
		Help:      "Distribution of #index lookups per query.",
		Buckets:   prometheus.DefBuckets,
	})
	preIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_series_pre_intersection_per_query",
		Help:      "Distribution of #series (pre intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^5) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 5),
	})
	postIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_series_post_intersection_per_query",
		Help:      "Distribution of #series (post intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^5) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 5),
	})
	chunksPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_chunks_per_query",
		Help:      "Distribution of #chunks per query.",
		// For 100k series for 7 week, could be 1.2m - 10*(8^6) = 2.6m.
		Buckets: prometheus.ExponentialBuckets(10, 8, 6),
	})
)

// seriesStore implements Store
type seriesStore struct {
	store
	cardinalityCache *cache.FifoCache
}

func newSeriesStore(cfg StoreConfig, schema Schema, storage StorageClient) (Store, error) {
	fetcher, err := NewChunkFetcher(cfg.CacheConfig, storage)
	if err != nil {
		return nil, err
	}

	return &seriesStore{
		store: store{
			cfg:     cfg,
			storage: storage,
			schema:  schema,
			Fetcher: fetcher,
		},
		cardinalityCache: cache.NewFifoCache("cardinality", cfg.CardinalityCacheSize, cfg.CardinalityCacheValidity),
	}, nil
}

// Get implements Store
func (c *seriesStore) Get(ctx context.Context, from, through model.Time, allMatchers ...*labels.Matcher) ([]Chunk, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.Get")
	defer log.Span.Finish()
	level.Debug(log).Log("from", from, "through", through, "matchers", len(allMatchers))

	// Validate the query is within reasonable bounds.
	shortcut, err := c.validateQuery(ctx, from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	// Ensure this query includes a metric name.
	metricNameMatcher, allMatchers, ok := extract.MetricNameMatcherFromMatchers(allMatchers)
	if !ok || metricNameMatcher.Type != labels.MatchEqual {
		return nil, fmt.Errorf("query must contain metric name")
	}
	level.Debug(log).Log("metric", metricNameMatcher.Value)

	// Fetch the series IDs from the index, based on non-empty matchers from
	// the query.
	_, matchers := util.SplitFiltersAndMatchers(allMatchers)
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, metricNameMatcher.Value, matchers)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("series-ids", len(seriesIDs))

	// Lookup the series in the index to get the chunks.
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, seriesIDs)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("chunk-ids", len(chunkIDs))

	chunks, err := c.convertChunkIDsToChunks(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}
	// Filter out chunks that are not in the selected time range.
	filtered, keys := filterChunksByTime(from, through, chunks)
	level.Debug(log).Log("chunks-post-filtering", len(chunks))
	chunksPerQuery.Observe(float64(len(filtered)))

	// Protect ourselves against OOMing.
	if len(chunkIDs) > c.cfg.QueryChunkLimit {
		err := fmt.Errorf("Query %v fetched too many chunks (%d > %d)", allMatchers, len(chunkIDs), c.cfg.QueryChunkLimit)
		level.Error(log).Log("err", err)
		return nil, err
	}

	// Now fetch the actual chunk data from Memcache / S3
	allChunks, err := c.FetchChunks(ctx, filtered, keys)
	if err != nil {
		level.Error(log).Log("err", err)
		return nil, err
	}

	// Filter out chunks based on the empty matchers in the query.
	filteredChunks := filterChunksByMatchers(allChunks, allMatchers)
	return filteredChunks, nil
}

func (c *seriesStore) lookupSeriesByMetricNameMatchers(ctx context.Context, from, through model.Time, metricName string, matchers []*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupSeriesByMetricNameMatchers", "metricName", metricName, "matchers", len(matchers))
	defer log.Span.Finish()

	// Just get series for metric if there are no matchers
	if len(matchers) == 0 {
		indexLookupsPerQuery.Observe(1)
		series, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, metricName, nil)
		if err != nil {
			preIntersectionPerQuery.Observe(float64(len(series)))
			postIntersectionPerQuery.Observe(float64(len(series)))
		}
		return series, err
	}

	// Otherwise get series which include other matchers
	incomingIDs := make(chan []string)
	incomingErrors := make(chan error)
	indexLookupsPerQuery.Observe(float64(len(matchers)))
	for _, matcher := range matchers {
		go func(matcher *labels.Matcher) {
			ids, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, metricName, matcher)
			if err != nil {
				incomingErrors <- err
				return
			}
			incomingIDs <- ids
		}(matcher)
	}

	// Receive series IDs from all matchers, intersect as we go.
	var ids []string
	var preIntersectionCount int
	var lastErr error
	var cardinalityExceededErrors int
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingIDs:
			preIntersectionCount += len(incoming)
			if ids == nil {
				ids = incoming
			} else {
				ids = intersectStrings(ids, incoming)
			}
		case err := <-incomingErrors:
			if err == errCardinalityExceeded {
				cardinalityExceededErrors++
			} else {
				lastErr = err
			}
		}
	}
	if cardinalityExceededErrors == len(matchers) {
		return nil, errCardinalityExceeded
	} else if lastErr != nil {
		return nil, lastErr
	}
	preIntersectionPerQuery.Observe(float64(preIntersectionCount))
	postIntersectionPerQuery.Observe(float64(len(ids)))

	level.Debug(log).Log("msg", "post intersection", "ids", len(ids))
	return ids, nil
}

func (c *seriesStore) lookupSeriesByMetricNameMatcher(ctx context.Context, from, through model.Time, metricName string, matcher *labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupSeriesByMetricNameMatcher", "metricName", metricName, "matcher", matcher)
	defer log.Span.Finish()

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	var queries []IndexQuery
	if matcher == nil {
		queries, err = c.schema.GetReadQueriesForMetric(from, through, userID, model.LabelValue(metricName))
	} else if matcher.Type != labels.MatchEqual {
		queries, err = c.schema.GetReadQueriesForMetricLabel(from, through, userID, model.LabelValue(metricName), model.LabelName(matcher.Name))
	} else {
		queries, err = c.schema.GetReadQueriesForMetricLabelValue(from, through, userID, model.LabelValue(metricName), model.LabelName(matcher.Name), model.LabelValue(matcher.Value))
	}
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("queries", len(queries))

	for _, query := range queries {
		value, ok := c.cardinalityCache.Get(ctx, query.HashValue)
		if !ok {
			continue
		}
		cardinality := value.(int)
		if cardinality > c.cfg.CardinalityLimit {
			return nil, errCardinalityExceeded
		}
	}

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("entries", len(entries))

	// TODO This is not correct, will overcount for queries > 24hrs
	keys := make([]string, 0, len(queries))
	values := make([]interface{}, 0, len(queries))
	for _, query := range queries {
		keys = append(keys, query.HashValue)
		values = append(values, len(entries))
	}
	c.cardinalityCache.Put(ctx, keys, values)

	if len(entries) > c.cfg.CardinalityLimit {
		return nil, errCardinalityExceeded
	}

	ids, err := c.parseIndexEntries(ctx, entries, matcher)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("ids", len(ids))

	return ids, nil
}

func (c *seriesStore) lookupChunksBySeries(ctx context.Context, from, through model.Time, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupChunksBySeries")
	defer log.Span.Finish()

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("seriesIDs", len(seriesIDs))

	queries := make([]IndexQuery, 0, len(seriesIDs))
	for _, seriesID := range seriesIDs {
		qs, err := c.schema.GetChunksForSeries(from, through, userID, []byte(seriesID))
		if err != nil {
			return nil, err
		}
		queries = append(queries, qs...)
	}
	level.Debug(log).Log("queries", len(queries))

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("entries", len(entries))

	result, err := c.parseIndexEntries(ctx, entries, nil)
	return result, err
}
