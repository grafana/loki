package tsdb

import (
	"context"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// implements stores.Index
type IndexClient struct {
	idx  Index
	opts IndexClientOptions
}

type IndexClientOptions struct {
	// Whether using bloom filters in the Stats() method
	// should be skipped. This helps probabilistically detect
	// duplicates when chunks are written to multiple
	// index buckets, which is of use in the (index-gateway|querier)
	// but not worth the memory costs in the ingesters.
	UseBloomFilters bool
}

func DefaultIndexClientOptions() IndexClientOptions {
	return IndexClientOptions{
		UseBloomFilters: true,
	}
}

type IndexStatsAccumulator interface {
	AddStream(fp model.Fingerprint)
	AddChunkStats(s index.ChunkStats)
	Stats() stats.Stats
}

func NewIndexClient(idx Index, opts IndexClientOptions) *IndexClient {
	return &IndexClient{
		idx:  idx,
		opts: opts,
	}
}

// TODO(owen-d): This is a hack for compatibility with how the current query-mapping works.
// Historically, Loki will read the index shard factor and the query planner will inject shard
// labels accordingly.
// In the future, we should use dynamic sharding in TSDB to determine the shard factors
// and we may no longer wish to send a shard label inside the queries,
// but rather expose it as part of the stores.Index interface
func cleanMatchers(matchers ...*labels.Matcher) ([]*labels.Matcher, *index.ShardAnnotation, error) {
	// first use withoutNameLabel to make a copy with the name label removed
	matchers = withoutNameLabel(matchers)
	s, shardLabelIndex, err := astmapper.ShardFromMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}

	var shard *index.ShardAnnotation
	if s != nil {
		matchers = append(matchers[:shardLabelIndex], matchers[shardLabelIndex+1:]...)
		shard = &index.ShardAnnotation{
			Shard: uint32(s.Shard),
			Of:    uint32(s.Of),
		}

		if err := shard.Validate(); err != nil {
			return nil, nil, err
		}
	}

	if len(matchers) == 0 {
		// hack to query all data
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	}

	return matchers, shard, err

}

// TODO(owen-d): synchronize logproto.ChunkRef and tsdb.ChunkRef so we don't have to convert.
// They share almost the same fields, so we can add the missing `KB` field to the proto and then
// use that within the tsdb package.
func (c *IndexClient) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "IndexClient.GetChunkRefs")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	var kvps []interface{}
	defer func() {
		log.Log(kvps...)
	}()

	matchers, shard, err := cleanMatchers(matchers...)
	kvps = append(kvps,
		"from", from.Time(),
		"through", through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"shard", shard,
		"cleanMatcherErr", err,
	)
	if err != nil {
		return nil, err
	}

	// TODO(owen-d): use a pool to reduce allocs here
	chks, err := c.idx.GetChunkRefs(ctx, userID, from, through, nil, shard, matchers...)
	kvps = append(kvps,
		"chunks", len(chks),
		"indexErr", err,
	)
	if err != nil {
		return nil, err
	}

	refs := make([]logproto.ChunkRef, 0, len(chks))
	for _, chk := range chks {
		refs = append(refs, logproto.ChunkRef{
			Fingerprint: uint64(chk.Fingerprint),
			UserID:      chk.User,
			From:        chk.Start,
			Through:     chk.End,
			Checksum:    chk.Checksum,
		})
	}

	return refs, err
}

func (c *IndexClient) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	matchers, shard, err := cleanMatchers(matchers...)
	if err != nil {
		return nil, err
	}

	xs, err := c.idx.Series(ctx, userID, from, through, nil, shard, matchers...)
	if err != nil {
		return nil, err
	}

	res := make([]labels.Labels, 0, len(xs))
	for _, x := range xs {
		res = append(res, x.Labels)
	}
	return res, nil
}

// tsdb no longer uses the __metric_name__="logs" hack, so we can ignore metric names!
func (c *IndexClient) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, _ string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	matchers, _, err := cleanMatchers(matchers...)
	if err != nil {
		return nil, err
	}
	return c.idx.LabelValues(ctx, userID, from, through, labelName, matchers...)
}

// tsdb no longer uses the __metric_name__="logs" hack, so we can ignore metric names!
func (c *IndexClient) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, _ string) ([]string, error) {
	return c.idx.LabelNames(ctx, userID, from, through)
}

func (c *IndexClient) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "IndexClient.Stats")
	defer sp.Finish()

	matchers, shard, err := cleanMatchers(matchers...)
	if err != nil {
		return nil, err
	}

	// split the query range to align with table intervals i.e. ObjectStorageIndexRequiredPeriod
	// This is to avoid explicitly deduping chunks by leveraging the table intervals.
	// The idea is to make each split process chunks that have start time >= start time of the table interval.
	// In other terms, table interval that contains start time of the chunk, owns it.
	// For e.g. if the table interval is 10s, and we have chunks 5-7, 8-12, 11-13.
	// Query with range 6-15 would be split into 6-10, 10-15.
	// query1 would process chunks 5-7, 8-12 and query2 would process chunks 11-13.
	// This check is not applied for first query of the split so that
	// we do not eliminate any chunks that overlaps the original query intervals but starts at the previous table.
	// For e.g. if the table interval is 10s, and we have chunks 5-7, 8-13, 14-13.
	// Query with range 11-12 should process chunk 8-13 even though its start time <= start time of table we will query for index.
	// The caveat here is that we will overestimate the data we will be processing if the index is not compacted yet
	// since it could have duplicate chunks when RF > 1
	var intervals []model.Interval
	util.ForInterval(config.ObjectStorageIndexRequiredPeriod, from.Time(), through.Time(), true, func(start, end time.Time) {
		intervals = append(intervals, model.Interval{
			Start: model.TimeFromUnixNano(start.UnixNano()),
			End:   model.TimeFromUnixNano(end.UnixNano()),
		})
	})

	var acc IndexStatsAccumulator
	if c.opts.UseBloomFilters {
		blooms := stats.BloomPool.Get()
		defer stats.BloomPool.Put(blooms)
		acc = blooms
	} else {
		acc = &stats.Stats{}
	}

	for _, interval := range intervals {
		if err := c.idx.Stats(ctx, userID, interval.Start, interval.End, acc, shard, nil, matchers...); err != nil {
			return nil, err
		}
	}

	if err != nil {
		return nil, err
	}
	res := acc.Stats()

	sp.LogKV(
		"from", from.Time(),
		"through", through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"shard", shard,
		"intervals", len(intervals),
		"streams", res.Streams,
		"chunks", res.Chunks,
		"bytes", res.Bytes,
		"entries", res.Entries,
	)

	return &res, nil
}

// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
// This is only used for GetSeries implementation.
// Todo we might want to pass it as a parameter to GetSeries instead.
func (c *IndexClient) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	c.idx.SetChunkFilterer(chunkFilter)
}

// TODO(owen-d): in the future, handle this by preventing passing the __name__="logs" label
// to TSDB indices at all.
func withoutNameLabel(matchers []*labels.Matcher) []*labels.Matcher {
	if len(matchers) == 0 {
		return nil
	}

	dst := make([]*labels.Matcher, 0, len(matchers)-1)
	for _, m := range matchers {
		if m.Name == labels.MetricName {
			continue
		}
		dst = append(dst, m)
	}

	return dst
}
