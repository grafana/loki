// SPDX-License-Identifier: AGPL-3.0-only

package indexcache

// type TracingIndexCache struct {
// 	c      IndexCache
// 	logger log.Logger
// }

// func NewTracingIndexCache(cache IndexCache, logger log.Logger) IndexCache {
// 	return &TracingIndexCache{
// 		c:      cache,
// 		logger: logger,
// 	}
// }

// func (t *TracingIndexCache) StorePostings(userID string, blockID ulid.ULID, l labels.Label, v []byte) {
// 	t.c.StorePostings(userID, blockID, l, v)
// }

// func (t *TracingIndexCache) FetchMultiPostings(ctx context.Context, userID string, blockID ulid.ULID, keys []labels.Label) (hits BytesResult) {
// 	t0 := time.Now()
// 	hits = t.c.FetchMultiPostings(ctx, userID, blockID, keys)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log(
// 		"msg", "IndexCache.FetchMultiPostings",
// 		"requested keys", len(keys),
// 		"cache hits", hits.Remaining(),
// 		"cache misses", len(keys)-hits.Remaining(),
// 		"time elapsed", time.Since(t0),
// 		"returned bytes", hits.Size(),
// 		"user_id", userID,
// 	)
// 	return hits
// }

// func (t *TracingIndexCache) StoreSeriesForRef(userID string, blockID ulid.ULID, id storage.SeriesRef, v []byte) {
// 	t.c.StoreSeriesForRef(userID, blockID, id, v)
// }

// func (t *TracingIndexCache) FetchMultiSeriesForRefs(ctx context.Context, userID string, blockID ulid.ULID, ids []storage.SeriesRef) (hits map[storage.SeriesRef][]byte, misses []storage.SeriesRef) {
// 	t0 := time.Now()
// 	hits, misses = t.c.FetchMultiSeriesForRefs(ctx, userID, blockID, ids)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log("msg", "IndexCache.FetchMultiSeriesForRefs",
// 		"requested series", len(ids),
// 		"cache hits", len(hits),
// 		"cache misses", len(misses),
// 		"time elapsed", time.Since(t0),
// 		"returned bytes", sumBytes(hits),
// 		"user_id", userID,
// 	)

// 	return hits, misses
// }

// func (t *TracingIndexCache) StoreExpandedPostings(userID string, blockID ulid.ULID, key LabelMatchersKey, postingsSelectionStrategy string, v []byte) {
// 	t.c.StoreExpandedPostings(userID, blockID, key, postingsSelectionStrategy, v)
// }

// func (t *TracingIndexCache) FetchExpandedPostings(ctx context.Context, userID string, blockID ulid.ULID, key LabelMatchersKey, postingsSelectionStrategy string) ([]byte, bool) {
// 	t0 := time.Now()
// 	data, found := t.c.FetchExpandedPostings(ctx, userID, blockID, key, postingsSelectionStrategy)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log(
// 		"msg", "IndexCache.FetchExpandedPostings",
// 		"requested key", key,
// 		"postings selection strategy", postingsSelectionStrategy,
// 		"found", found,
// 		"time elapsed", time.Since(t0),
// 		"returned bytes", len(data),
// 		"user_id", userID,
// 	)

// 	return data, found
// }

// func (t *TracingIndexCache) StoreSeriesForPostings(userID string, blockID ulid.ULID, shard *sharding.ShardSelector, postingsKey PostingsKey, v []byte) {
// 	t.c.StoreSeriesForPostings(userID, blockID, shard, postingsKey, v)
// }

// func (t *TracingIndexCache) FetchSeriesForPostings(ctx context.Context, userID string, blockID ulid.ULID, shard *sharding.ShardSelector, postingsKey PostingsKey) ([]byte, bool) {
// 	t0 := time.Now()
// 	data, found := t.c.FetchSeriesForPostings(ctx, userID, blockID, shard, postingsKey)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log(
// 		"msg", "IndexCache.FetchSeriesForPostings",
// 		"shard", shardKey(shard),
// 		"found", found,
// 		"time_elapsed", time.Since(t0),
// 		"returned_bytes", len(data),
// 		"user_id", userID,
// 		"postings_key", postingsKey,
// 	)

// 	return data, found
// }

// func (t *TracingIndexCache) StoreLabelNames(userID string, blockID ulid.ULID, matchersKey LabelMatchersKey, v []byte) {
// 	t.c.StoreLabelNames(userID, blockID, matchersKey, v)
// }

// func (t *TracingIndexCache) FetchLabelNames(ctx context.Context, userID string, blockID ulid.ULID, matchersKey LabelMatchersKey) ([]byte, bool) {
// 	t0 := time.Now()
// 	data, found := t.c.FetchLabelNames(ctx, userID, blockID, matchersKey)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log(
// 		"msg", "IndexCache.FetchLabelNames",
// 		"requested key", matchersKey,
// 		"found", found,
// 		"time elapsed", time.Since(t0),
// 		"returned bytes", len(data),
// 		"user_id", userID,
// 	)

// 	return data, found
// }

// func (t *TracingIndexCache) StoreLabelValues(userID string, blockID ulid.ULID, labelName string, matchersKey LabelMatchersKey, v []byte) {
// 	t.c.StoreLabelValues(userID, blockID, labelName, matchersKey, v)
// }

// func (t *TracingIndexCache) FetchLabelValues(ctx context.Context, userID string, blockID ulid.ULID, labelName string, matchersKey LabelMatchersKey) ([]byte, bool) {
// 	t0 := time.Now()
// 	data, found := t.c.FetchLabelValues(ctx, userID, blockID, labelName, matchersKey)

// 	spanLogger := spanlogger.FromContext(ctx, t.logger)
// 	level.Debug(spanLogger).Log(
// 		"msg", "IndexCache.FetchLabelValues",
// 		"label name", labelName,
// 		"requested key", matchersKey,
// 		"found", found,
// 		"time elapsed", time.Since(t0),
// 		"returned bytes", len(data),
// 		"user_id", userID,
// 	)

// 	return data, found
// }
