package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// implements stores.Index
type IndexClient struct {
	idx Index
}

func NewIndexClient(idx Index) *IndexClient {
	return &IndexClient{
		idx: idx,
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
	log, ctx := spanlogger.New(ctx, "IndexClient.GetChunkRefs")
	defer log.Span.Finish()

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
	matchers, shard, err := cleanMatchers(matchers...)
	if err != nil {
		return nil, err
	}

	blooms := stats.BloomPool.Get()
	defer stats.BloomPool.Put(blooms)
	blooms, err = c.idx.Stats(ctx, userID, from, through, blooms, shard, matchers...)

	if err != nil {
		return nil, err
	}
	res := blooms.Stats()

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
