package series

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

// IndexGatewayClientStore implements pkg/storage/stores/index.ReaderWriter
type IndexGatewayClientStore struct {
	client logproto.IndexGatewayClient
	logger log.Logger
}

func NewIndexGatewayClientStore(client logproto.IndexGatewayClient, logger log.Logger) index.ReaderWriter {
	return &IndexGatewayClientStore{
		client: client,
		logger: logger,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, _ string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	response, err := c.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: allMatchers}).String(),
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
func (c *IndexGatewayClientStore) LabelNamesForMetricName(ctx context.Context, _ string, from, through model.Time, metricName string) ([]string, error) {
	resp, err := c.client.LabelNamesForMetricName(ctx, &logproto.LabelNamesForMetricNameRequest{
		MetricName: metricName,
		From:       from,
		Through:    through,
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

func (c *IndexGatewayClientStore) SeriesVolume(ctx context.Context, _ string, from, through model.Time, limit int32, targetLabels []string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return c.client.GetSeriesVolume(ctx, &logproto.VolumeRequest{
		From:         from,
		Through:      through,
		Matchers:     (&syntax.MatchersExpr{Mts: matchers}).String(),
		Limit:        limit,
		TargetLabels: targetLabels,
	})
}

func (c *IndexGatewayClientStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {
	level.Warn(c.logger).Log("msg", "SetChunkFilterer called on index gateway client store, but it does not support it")
}

func (c *IndexGatewayClientStore) IndexChunk(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return fmt.Errorf("index writes not supported on index gateway client")
}
