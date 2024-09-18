package series

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

// NB(owen-d): mostly modeled off of the proto-generated `logproto.IndexGatewayClient`,
// but decoupled from explicit GRPC dependencies to work well with streaming grpc methods
type GatewayClient interface {
	GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error)
	GetSeries(ctx context.Context, in *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error)
	LabelNamesForMetricName(ctx context.Context, in *logproto.LabelNamesForMetricNameRequest) (*logproto.LabelResponse, error)
	LabelValuesForMetricName(ctx context.Context, in *logproto.LabelValuesForMetricNameRequest) (*logproto.LabelResponse, error)
	GetStats(ctx context.Context, in *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error)
	GetVolume(ctx context.Context, in *logproto.VolumeRequest) (*logproto.VolumeResponse, error)

	GetShards(ctx context.Context, in *logproto.ShardsRequest) (*logproto.ShardsResponse, error)
}

// IndexGatewayClientStore implements pkg/storage/stores/index.ReaderWriter
type IndexGatewayClientStore struct {
	client GatewayClient
	logger log.Logger
}

func NewIndexGatewayClientStore(client GatewayClient, logger log.Logger) *IndexGatewayClientStore {
	return &IndexGatewayClientStore{
		client: client,
		logger: logger,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, _ string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error) {
	response, err := c.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: predicate.Matchers}).String(),
		Plan:     predicate.Plan(),
	})
	if err != nil {
		return nil, err
	}

	result := make([]logproto.ChunkRef, len(response.Refs))
	for i, ref := range response.Refs {
		result[i] = *ref
	}

	return result, nil
}

func (c *IndexGatewayClientStore) GetSeries(ctx context.Context, _ string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	resp, err := c.client.GetSeries(ctx, &logproto.GetSeriesRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if err != nil {
		return nil, err
	}

	result := make([]labels.Labels, len(resp.Series))
	for i, s := range resp.Series {
		result[i] = logproto.FromLabelAdaptersToLabels(s.Labels)
	}

	return result, nil
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *IndexGatewayClientStore) LabelNamesForMetricName(ctx context.Context, _ string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error) {
	resp, err := c.client.LabelNamesForMetricName(ctx, &logproto.LabelNamesForMetricNameRequest{
		MetricName: metricName,
		From:       from,
		Through:    through,
		Matchers:   (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *IndexGatewayClientStore) LabelValuesForMetricName(ctx context.Context, _ string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	resp, err := c.client.LabelValuesForMetricName(ctx, &logproto.LabelValuesForMetricNameRequest{
		MetricName: metricName,
		LabelName:  labelName,
		From:       from,
		Through:    through,
		Matchers:   (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *IndexGatewayClientStore) Stats(ctx context.Context, _ string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	return c.client.GetStats(ctx, &logproto.IndexStatsRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
}

func (c *IndexGatewayClientStore) Volume(ctx context.Context, _ string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return c.client.GetVolume(ctx, &logproto.VolumeRequest{
		From:         from,
		Through:      through,
		Matchers:     (&syntax.MatchersExpr{Mts: matchers}).String(),
		Limit:        limit,
		TargetLabels: targetLabels,
		AggregateBy:  aggregateBy,
	})
}

func (c *IndexGatewayClientStore) GetShards(
	ctx context.Context,
	_ string,
	from, through model.Time,
	targetBytesPerShard uint64,
	predicate chunk.Predicate,
) (*logproto.ShardsResponse, error) {
	resp, err := c.client.GetShards(ctx, &logproto.ShardsRequest{
		From:                from,
		Through:             through,
		Query:               predicate.Plan().AST.String(),
		TargetBytesPerShard: targetBytesPerShard,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *IndexGatewayClientStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {
	level.Warn(c.logger).Log("msg", "SetChunkFilterer called on index gateway client store, but it does not support it")
}

func (c *IndexGatewayClientStore) IndexChunk(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return fmt.Errorf("index writes not supported on index gateway client")
}

// IndexGatewayClientStore does not implement tsdb.ForSeries;
// that is implemented by the index-gws themselves and will be
// called during the `GetShards() invocation`
func (c *IndexGatewayClientStore) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}
