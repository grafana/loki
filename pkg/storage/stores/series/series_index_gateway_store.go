package series

import (
	"context"

	"github.com/gogo/status"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type IndexGatewayClientStore struct {
	client IndexGatewayClient
	IndexStore
}

type IndexGatewayClient interface {
	GetChunkRef(ctx context.Context, in *indexgatewaypb.GetChunkRefRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetChunkRefResponse, error)
	GetSeries(ctx context.Context, in *indexgatewaypb.GetSeriesRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetSeriesResponse, error)
	LabelNamesForMetricName(ctx context.Context, in *indexgatewaypb.LabelNamesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
	LabelValuesForMetricName(ctx context.Context, in *indexgatewaypb.LabelValuesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
}

func NewIndexGatewayClientStore(client IndexGatewayClient, index IndexStore) IndexStore {
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
		if isUnimplementedCallError(err) {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			return c.IndexStore.GetChunkRefs(ctx, userID, from, through, allMatchers...)
		}
		return nil, err
	}
	result := make([]logproto.ChunkRef, len(response.Refs))
	for i, ref := range response.Refs {
		result[i] = *ref
	}

	return result, nil
}

func (c *IndexGatewayClientStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	resp, err := c.client.GetSeries(ctx, &indexgatewaypb.GetSeriesRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if err != nil {
		if isUnimplementedCallError(err) {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			return c.IndexStore.GetSeries(ctx, userID, from, through, matchers...)
		}
		return nil, err
	}

	result := make([]labels.Labels, len(resp.Series))
	for i, s := range resp.Series {
		result[i] = logproto.FromLabelAdaptersToLabels(s.Labels)
	}

	return result, nil
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *IndexGatewayClientStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	resp, err := c.client.LabelNamesForMetricName(ctx, &indexgatewaypb.LabelNamesForMetricNameRequest{
		MetricName: metricName,
		From:       from,
		Through:    through,
	})
	if isUnimplementedCallError(err) {
		// Handle communication with older index gateways gracefully, by falling back to the index store calls.
		return c.IndexStore.LabelNamesForMetricName(ctx, userID, from, through, metricName)
	}
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *IndexGatewayClientStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	resp, err := c.client.LabelValuesForMetricName(ctx, &indexgatewaypb.LabelValuesForMetricNameRequest{
		MetricName: metricName,
		LabelName:  labelName,
		From:       from,
		Through:    through,
		Matchers:   (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if isUnimplementedCallError(err) {
		// Handle communication with older index gateways gracefully, by falling back to the index store calls.
		return c.IndexStore.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
	}
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

// isUnimplementedCallError tells if the GRPC error is a gRPC error with code Unimplemented.
func isUnimplementedCallError(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return (s.Code() == codes.Unimplemented)
}
