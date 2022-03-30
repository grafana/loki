package chunk

import (
	"context"
	"sort"
	"sync"

	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/chunk/index"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/extract"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type fetcher interface {
	FetchChunks(ctx context.Context, chunks []encoding.Chunk, keys []string) ([]encoding.Chunk, error)
}

type seriesIndexStore struct {
	schema    index.SeriesStoreSchema
	index     index.IndexClient
	schemaCfg config.SchemaConfig
	fetcher   fetcher
}

func newSeriesIndexStore(schemaCfg config.SchemaConfig, schema index.SeriesStoreSchema, index index.IndexClient, fetcher fetcher) *seriesIndexStore {
	return &seriesIndexStore{
		schema:    schema,
		index:     index,
		schemaCfg: schemaCfg,
		fetcher:   fetcher,
	}
}

func (c *seriesIndexStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	log := util_log.WithContext(ctx, util_log.Logger)
	// Check there is a metric name matcher of type equal,
	metricNameMatcher, matchers, ok := extract.MetricNameMatcherFromMatchers(allMatchers)
	if !ok || metricNameMatcher.Type != labels.MatchEqual {
		return nil, ErrQueryMustContainMetricName
	}
	metricName := metricNameMatcher.Value
	// Fetch the series IDs from the index, based on non-empty matchers from
	// the query.
	_, matchers = util.SplitFiltersAndMatchers(matchers)
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, userID, metricName, matchers)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("series-ids", len(seriesIDs))

	// Lookup the series in the index to get the chunks.
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		level.Error(log).Log("msg", "lookupChunksBySeries", "err", err)
		return nil, err
	}
	level.Debug(log).Log("chunk-ids", len(chunkIDs))

	chunks, err := c.convertChunkIDsToChunkRefs(ctx, userID, chunkIDs)
	if err != nil {
		level.Error(log).Log("op", "convertChunkIDsToChunks", "err", err)
		return nil, err
	}

	chunks = filterChunkRefsByTime(from, through, chunks)
	level.Debug(log).Log("chunks-post-filtering", len(chunks))
	chunksPerQuery.Observe(float64(len(chunks)))

	// We should return an empty chunks slice if there are no chunks.
	if len(chunks) == 0 {
		return []logproto.ChunkRef{}, nil
	}

	return chunks, nil
}

func (c *seriesIndexStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.SeriesIdentifier, error) {
	_, err := c.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}
	// download one per series and merge
	// group chunks by series
	// chunksBySeries := partitionBySeriesChunks(lazyChunks)

	// firstChunksPerSeries := make([]*LazyChunk, 0, len(chunksBySeries))

	// // discard all but one chunk per series
	// for _, chks := range chunksBySeries {
	// 	firstChunksPerSeries = append(firstChunksPerSeries, chks[0][0])
	// }

	// results := make(logproto.SeriesIdentifiers, 0, len(firstChunksPerSeries))

	// // bound concurrency
	// groups := make([][]*LazyChunk, 0, len(firstChunksPerSeries)/s.cfg.MaxChunkBatchSize+1)

	// split := s.cfg.MaxChunkBatchSize
	// if len(firstChunksPerSeries) < split {
	// 	split = len(firstChunksPerSeries)
	// }

	// var chunkFilterer ChunkFilterer
	// if s.chunkFilterer != nil {
	// 	chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	// }

	// for split > 0 {
	// 	groups = append(groups, firstChunksPerSeries[:split])
	// 	firstChunksPerSeries = firstChunksPerSeries[split:]
	// 	if len(firstChunksPerSeries) < split {
	// 		split = len(firstChunksPerSeries)
	// 	}
	// }

	// for _, group := range groups {
	// 	err = fetchLazyChunks(ctx, s.schemaCfg.SchemaConfig, group)
	// 	if err != nil {
	// 		return nil, err
	// 	}

	// outer:
	// 	for _, chk := range group {
	// 		for _, matcher := range matchers {
	// 			if matcher.Name == astmapper.ShardLabel || matcher.Name == labels.MetricName {
	// 				continue
	// 			}
	// 			if !matcher.Matches(chk.Chunk.Metric.Get(matcher.Name)) {
	// 				continue outer
	// 			}
	// 		}

	// 		if chunkFilterer != nil && chunkFilterer.ShouldFilter(chk.Chunk.Metric) {
	// 			continue outer
	// 		}

	// 		m := chk.Chunk.Metric.Map()
	// 		delete(m, labels.MetricName)
	// 		results = append(results, logproto.SeriesIdentifier{
	// 			Labels: m,
	// 		})
	// 	}
	// }
	// sort.Sort(results)
	// return results, nil
	return nil, nil
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *seriesIndexStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelNamesForMetricName")
	defer log.Span.Finish()

	// Fetch the series IDs from the index
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, userID, metricName, nil)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("series-ids", len(seriesIDs))

	// Lookup the series in the index to get label names.
	labelNames, err := c.lookupLabelNamesBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		// looking up metrics by series is not supported falling back on chunks
		if err == ErrNotSupported {
			return c.lookupLabelNamesByChunks(ctx, from, through, userID, seriesIDs)
		}
		level.Error(log).Log("msg", "lookupLabelNamesBySeries", "err", err)
		return nil, err
	}
	level.Debug(log).Log("labelNames", len(labelNames))

	return labelNames, nil
}

func (c *seriesIndexStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelValuesForMetricName")
	defer log.Span.Finish()

	if len(matchers) != 0 {
		return c.labelValuesForMetricNameWithMatchers(ctx, userID, from, through, metricName, labelName, matchers...)
	}

	level.Debug(log).Log("from", from, "through", through, "metricName", metricName, "labelName", labelName)

	queries, err := c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}

	var result UniqueStrings
	for _, entry := range entries {
		_, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}
		result.Add(string(labelValue))
	}
	return result.Strings(), nil
}

// LabelValuesForMetricName retrieves all label values for a single label name and metric name.
func (c *seriesIndexStore) labelValuesForMetricNameWithMatchers(ctx context.Context, userID string, from, through model.Time, metricName, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	// Otherwise get series which include other matchers
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, userID, metricName, matchers)
	if err != nil {
		return nil, err
	}
	seriesIDsSet := make(map[string]struct{}, len(seriesIDs))
	for _, i := range seriesIDs {
		seriesIDsSet[i] = struct{}{}
	}

	contains := func(id string) bool {
		_, ok := seriesIDsSet[id]
		return ok
	}

	// Fetch label values for label name that are part of the filtered chunks
	queries, err := c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}
	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}

	result := NewUniqueStrings(len(entries))
	for _, entry := range entries {
		seriesID, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}
		if contains(seriesID) {
			result.Add(string(labelValue))
		}
	}

	return result.Strings(), nil
}

func (c *seriesIndexStore) lookupSeriesByMetricNameMatchers(ctx context.Context, from, through model.Time, userID, metricName string, matchers []*labels.Matcher) ([]string, error) {
	// Check if one of the labels is a shard annotation, pass that information to lookupSeriesByMetricNameMatcher,
	// and remove the label.
	shard, shardLabelIndex, err := astmapper.ShardFromMatchers(matchers)
	if err != nil {
		return nil, err
	}

	if shard != nil {
		matchers = append(matchers[:shardLabelIndex], matchers[shardLabelIndex+1:]...)
	}

	// Just get series for metric if there are no matchers
	if len(matchers) == 0 {
		indexLookupsPerQuery.Observe(1)
		series, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, nil, shard)
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
			ids, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, matcher, shard)
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
	var cardinalityExceededError CardinalityExceededError
	var initialized bool
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingIDs:
			preIntersectionCount += len(incoming)
			if !initialized {
				ids = incoming
				initialized = true
			} else {
				ids = intersectStrings(ids, incoming)
			}
		case err := <-incomingErrors:
			// The idea is that if we have 2 matchers, and if one returns a lot of
			// series and the other returns only 10 (a few), we don't lookup the first one at all.
			// We just manually filter through the 10 series again using "filterChunksByMatchers",
			// saving us from looking up and intersecting a lot of series.
			if e, ok := err.(CardinalityExceededError); ok {
				cardinalityExceededErrors++
				cardinalityExceededError = e
			} else {
				lastErr = err
			}
		}
	}

	// But if every single matcher returns a lot of series, then it makes sense to abort the query.
	if cardinalityExceededErrors == len(matchers) {
		return nil, cardinalityExceededError
	} else if lastErr != nil {
		return nil, lastErr
	}
	preIntersectionPerQuery.Observe(float64(preIntersectionCount))
	postIntersectionPerQuery.Observe(float64(len(ids)))

	level.Debug(util_log.WithContext(ctx, util_log.Logger)).
		Log("msg", "post intersection", "matchers", len(matchers), "ids", len(ids))
	return ids, nil
}

func (c *seriesIndexStore) lookupSeriesByMetricNameMatcher(ctx context.Context, from, through model.Time, userID, metricName string, matcher *labels.Matcher, shard *astmapper.ShardAnnotation) ([]string, error) {
	return c.lookupIdsByMetricNameMatcher(ctx, from, through, userID, metricName, matcher, func(queries []IndexQuery) []IndexQuery {
		return c.schema.FilterReadQueries(queries, shard)
	})
}

func (c *seriesIndexStore) lookupIdsByMetricNameMatcher(ctx context.Context, from, through model.Time, userID, metricName string, matcher *labels.Matcher, filter func([]IndexQuery) []IndexQuery) ([]string, error) {
	var err error
	var queries []index.IndexQuery
	var labelName string
	if matcher == nil {
		queries, err = c.schema.GetReadQueriesForMetric(from, through, userID, metricName)
	} else if matcher.Type == labels.MatchEqual {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, matcher.Name, matcher.Value)
	} else {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, matcher.Name)
	}
	if err != nil {
		return nil, err
	}
	unfilteredQueries := len(queries)

	if filter != nil {
		queries = filter(queries)
	}

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if e, ok := err.(CardinalityExceededError); ok {
		e.MetricName = metricName
		e.LabelName = labelName
		return nil, e
	} else if err != nil {
		return nil, err
	}

	ids, err := parseIndexEntries(ctx, entries, matcher)
	if err != nil {
		return nil, err
	}
	level.Debug(util_log.WithContext(ctx, util_log.Logger)).
		Log(
			"msg", "Store.lookupIdsByMetricNameMatcher",
			"matcher", formatMatcher(matcher),
			"queries", unfilteredQueries,
			"filteredQueries", len(queries),
			"entries", len(entries),
			"ids", len(ids),
		)

	return ids, nil
}

func parseIndexEntries(_ context.Context, entries []IndexEntry, matcher *labels.Matcher) ([]string, error) {
	// Nothing to do if there are no entries.
	if len(entries) == 0 {
		return nil, nil
	}

	matchSet := map[string]struct{}{}
	if matcher != nil && matcher.Type == labels.MatchRegexp {
		set := FindSetMatches(matcher.Value)
		for _, v := range set {
			matchSet[v] = struct{}{}
		}
	}

	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		chunkKey, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}

		// If the matcher is like a set (=~"a|b|c|d|...") and
		// the label value is not in that set move on.
		if len(matchSet) > 0 {
			if _, ok := matchSet[string(labelValue)]; !ok {
				continue
			}

			// If its in the set, then add it to set, we don't need to run
			// matcher on it again.
			result = append(result, chunkKey)
			continue
		}

		if matcher != nil && !matcher.Matches(string(labelValue)) {
			continue
		}
		result = append(result, chunkKey)
	}
	// Return ids sorted and deduped because they will be merged with other sets.
	sort.Strings(result)
	result = uniqueStrings(result)
	return result, nil
}

func (c *seriesIndexStore) lookupEntriesByQueries(ctx context.Context, queries []IndexQuery) ([]IndexEntry, error) {
	// Nothing to do if there are no queries.
	if len(queries) == 0 {
		return nil, nil
	}

	var lock sync.Mutex
	var entries []IndexEntry
	err := c.index.QueryPages(ctx, queries, func(query IndexQuery, resp ReadBatch) bool {
		iter := resp.Iterator()
		lock.Lock()
		for iter.Next() {
			entries = append(entries, IndexEntry{
				TableName:  query.TableName,
				HashValue:  query.HashValue,
				RangeValue: iter.RangeValue(),
				Value:      iter.Value(),
			})
		}
		lock.Unlock()
		return true
	})
	if err != nil {
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "error querying storage", "err", err)
	}
	return entries, err
}

func (c *seriesIndexStore) lookupLabelNamesBySeries(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupLabelNamesBySeries")
	defer log.Span.Finish()

	level.Debug(log).Log("seriesIDs", len(seriesIDs))
	queries := make([]IndexQuery, 0, len(seriesIDs))
	for _, seriesID := range seriesIDs {
		qs, err := c.schema.GetLabelNamesForSeries(from, through, userID, []byte(seriesID))
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

	var result UniqueStrings
	result.Add(model.MetricNameLabel)
	for _, entry := range entries {
		lbs := []string{}
		err := jsoniter.ConfigFastest.Unmarshal(entry.Value, &lbs)
		if err != nil {
			return nil, err
		}
		result.Add(lbs...)
	}
	return result.Strings(), nil
}

func (c *seriesIndexStore) lookupLabelNamesByChunks(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupLabelNamesByChunks")
	defer log.Span.Finish()

	// Lookup the series in the index to get the chunks.
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		level.Error(log).Log("msg", "lookupChunksBySeries", "err", err)
		return nil, err
	}
	level.Debug(log).Log("chunk-ids", len(chunkIDs))

	chunks, err := c.convertChunkIDsToChunks(ctx, userID, chunkIDs)
	if err != nil {
		level.Error(log).Log("err", "convertChunkIDsToChunks", "err", err)
		return nil, err
	}

	// Filter out chunks that are not in the selected time range and keep a single chunk per fingerprint
	filtered := filterChunksByTime(from, through, chunks)
	filtered, keys := filterChunksByUniqueFingerprint(c.schemaCfg, filtered)
	level.Debug(log).Log("Chunks post filtering", len(chunks))

	chunksPerQuery.Observe(float64(len(filtered)))

	// Now fetch the actual chunk data from Memcache / S3
	allChunks, err := c.fetcher.FetchChunks(ctx, filtered, keys)
	if err != nil {
		level.Error(log).Log("msg", "FetchChunks", "err", err)
		return nil, err
	}
	return labelNamesFromChunks(allChunks), nil
}

func (c *seriesIndexStore) lookupChunksBySeries(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	queries := make([]IndexQuery, 0, len(seriesIDs))
	for _, seriesID := range seriesIDs {
		qs, err := c.schema.GetChunksForSeries(from, through, userID, []byte(seriesID))
		if err != nil {
			return nil, err
		}
		queries = append(queries, qs...)
	}

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}
	level.Debug(util_log.WithContext(ctx, util_log.Logger)).Log(
		"msg", "SeriesStore.lookupChunksBySeries",
		"seriesIDs", len(seriesIDs),
		"queries", len(queries),
		"entries", len(entries))

	result, err := parseIndexEntries(ctx, entries, nil)
	return result, err
}

func (c *seriesIndexStore) convertChunkIDsToChunks(_ context.Context, userID string, chunkIDs []string) ([]Chunk, error) {
	chunkSet := make([]Chunk, 0, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		chunk, err := ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, err
		}
		chunkSet = append(chunkSet, chunk)
	}

	return chunkSet, nil
}

func (c *seriesIndexStore) convertChunkIDsToChunkRefs(_ context.Context, userID string, chunkIDs []string) ([]logproto.ChunkRef, error) {
	chunkSet := make([]logproto.ChunkRef, 0, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		chunk, err := ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, err
		}
		chunkSet = append(chunkSet, chunk.ChunkRef)
	}

	return chunkSet, nil
}
