package series

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type IndexGatewayClientStore struct {
	client indexgatewaypb.IndexGatewayClient
	*IndexStore
}

func NewIndexGatewayClientStore(client indexgatewaypb.IndexGatewayClient, index *IndexStore) *IndexGatewayClientStore {
	return &IndexGatewayClientStore{
		client:     client,
		IndexStore: index,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	response, err := c.client.GetChunkRef(ctx, &indexgatewaypb.GetChunkRefRequest{
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

func (c *IndexGatewayClientStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	refs, err := c.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}
	return c.chunksToSeries(ctx, refs, matchers)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *IndexGatewayClientStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	resp, err := c.client.LabelNamesForMetricName(ctx, &indexgatewaypb.LabelNamesForMetricNameRequest{
		MetricName: metricName,
		From:       from,
		Through:    through,
	})

	return resp.Values, err
}

func (c *IndexGatewayClientStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	resp, err := c.client.LabelValuesForMetricName(ctx, &indexgatewaypb.LabelValuesForMetricNameRequest{
		MetricName: metricName,
		LabelName:  labelName,
		From:       from,
		Through:    through,
		Matchers:   (&syntax.MatchersExpr{Mts: matchers}).String(),
	})

	return resp.Values, err
}
