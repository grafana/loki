package tsdb

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/logql"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util"
)

// implements stores.Index
type IndexClient struct {
	idx    Index
	opts   IndexClientOptions
	limits Limits
}

type IndexClientOptions struct {
	// Whether using bloom filters in the Stats() method
	// should be skipped. This helps probabilistically detect
	// duplicates when chunks are written to multiple
	// index buckets, which is of use in the (index-gateway|querier)
	// but not worth the memory costs in the ingesters.
	// NB(owen-d): This is NOT the bloom-filter feature developed late 2023 onwards,
	// but a smaller bloom filter used internally for probabalistic deduping of series counts
	// in the index stats() method across index buckets (which can have the same series)
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

type VolumeAccumulator interface {
	AddVolume(string, uint64) error
	Volumes() *logproto.VolumeResponse
}

type Limits interface {
	VolumeMaxSeries(string) int
}

func NewIndexClient(idx Index, opts IndexClientOptions, l Limits) *IndexClient {
	return &IndexClient{
		idx:    idx,
		opts:   opts,
		limits: l,
	}
}

func shardFromMatchers(matchers []*labels.Matcher) (cleaned []*labels.Matcher, res logql.Shard, found bool, err error) {
	for i, matcher := range matchers {
		if matcher.Name == astmapper.ShardLabel && matcher.Type == labels.MatchEqual {
			shard, _, err := logql.ParseShard(matcher.Value)
			if err != nil {
				return nil, shard, true, err
			}
			return append(matchers[:i], matchers[i+1:]...), shard, true, nil
		}
	}

	return matchers, logql.Shard{}, false, nil
}

// TODO(owen-d): This is a hack for compatibility with how the current query-mapping works.
// Historically, Loki will read the index shard factor and the query planner will inject shard
// labels accordingly.
// In the future, we should use dynamic sharding in TSDB to determine the shard factors
// and we may no longer wish to send a shard label inside the queries,
// but rather expose it as part of the stores.Index interface
func cleanMatchers(matchers ...*labels.Matcher) ([]*labels.Matcher, index.FingerprintFilter, error) {
	// first use withoutNameLabel to make a copy with the name label removed
	matchers = withoutNameLabel(matchers)

	matchers, shard, found, err := shardFromMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}

	if len(matchers) == 0 {
		// hack to query all data
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	}

	if found {
		return matchers, &shard, nil
	}
	return matchers, nil, nil
}

// TODO(owen-d): synchronize logproto.ChunkRef and tsdb.ChunkRef so we don't have to convert.
// They share almost the same fields, so we can add the missing `KB` field to the proto and then
// use that within the tsdb package.
func (c *IndexClient) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error) {
	matchers, shard, err := cleanMatchers(predicate.Matchers...)
	if err != nil {
		return nil, err
	}

	// TODO(owen-d): use a pool to reduce allocs here
	chks, err := c.idx.GetChunkRefs(ctx, userID, from, through, nil, shard, matchers...)
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
func (c *IndexClient) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, _ string, matchers ...*labels.Matcher) ([]string, error) {
	return c.idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (c *IndexClient) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
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

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV(
			"function", "IndexClient.Stats",
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
	}
	return &res, nil
}

func (c *IndexClient) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "IndexClient.Volume")
	defer sp.Finish()

	matchers, shard, err := cleanMatchers(matchers...)
	if err != nil {
		return nil, err
	}

	// Split by interval to query the index in parallel
	var intervals []model.Interval
	util.ForInterval(config.ObjectStorageIndexRequiredPeriod, from.Time(), through.Time(), true, func(start, end time.Time) {
		intervals = append(intervals, model.Interval{
			Start: model.TimeFromUnixNano(start.UnixNano()),
			End:   model.TimeFromUnixNano(end.UnixNano()),
		})
	})

	acc := seriesvolume.NewAccumulator(limit, c.limits.VolumeMaxSeries(userID))
	for _, interval := range intervals {
		if err := c.idx.Volume(ctx, userID, interval.Start, interval.End, acc, shard, nil, targetLabels, aggregateBy, matchers...); err != nil {
			return nil, err
		}
	}

	sp.LogKV(
		"from", from.Time(),
		"through", through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"shard", shard,
		"intervals", len(intervals),
		"limit", limit,
		"aggregateBy", aggregateBy,
	)

	if err != nil {
		return nil, err
	}

	return acc.Volumes(), nil
}

func (c *IndexClient) GetShards(ctx context.Context, userID string, from, through model.Time, targetBytesPerShard uint64, predicate chunk.Predicate) (*logproto.ShardsResponse, error) {
	// TODO(owen-d): perf, this is expensive :(
	var mtx sync.Mutex

	m := make(map[model.Fingerprint]index.ChunkMetas, 1024)
	if err := c.idx.ForSeries(ctx, userID, v1.FullBounds, from, through, func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		mtx.Lock()
		m[fp] = append(m[fp], chks...)
		mtx.Unlock()
		return false
	}, predicate.Matchers...); err != nil {
		return nil, err
	}

	resp := &logproto.ShardsResponse{}

	series := sharding.SizedFPs(sharding.SizedFPsPool.Get(len(m)))
	defer sharding.SizedFPsPool.Put(series)

	for fp, chks := range m {
		x := sharding.SizedFP{Fp: fp}
		deduped := chks.Finalize()
		x.Stats.Chunks = uint64(len(deduped))
		resp.Statistics.Index.TotalChunks += int64(len(deduped))

		for _, chk := range deduped {
			x.Stats.Entries += uint64(chk.Entries)
			x.Stats.Bytes += uint64(chk.KB << 10)
		}

		series = append(series, x)
	}
	sort.Sort(series)
	resp.Shards = series.ShardsFor(targetBytesPerShard)

	return resp, nil
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

func (c *IndexClient) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return c.idx, true
}
