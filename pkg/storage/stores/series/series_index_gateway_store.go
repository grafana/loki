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
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type IndexGatewayClientStore struct {
	client IndexGatewayClient
	// fallbackStore is used only to keep index gateways backwards compatible.
	// Previously index gateways would only serve index rows from boltdb-shipper files.
	// tsdb also supports configuring index gateways but there is no concept of serving index rows so
	// the fallbackStore could be nil and should be checked before use
	fallbackStore IndexStore
}

type IndexGatewayClient interface {
	GetChunkRef(ctx context.Context, in *indexgatewaypb.GetChunkRefRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetChunkRefResponse, error)
	GetSeries(ctx context.Context, in *indexgatewaypb.GetSeriesRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetSeriesResponse, error)
	LabelNamesForMetricName(ctx context.Context, in *indexgatewaypb.LabelNamesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
	LabelValuesForMetricName(ctx context.Context, in *indexgatewaypb.LabelValuesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
	GetStats(ctx context.Context, req *indexgatewaypb.IndexStatsRequest, opts ...grpc.CallOption) (*indexgatewaypb.IndexStatsResponse, error)
}

func NewIndexGatewayClientStore(client IndexGatewayClient, fallbackStore IndexStore) IndexStore {
	return &IndexGatewayClientStore{
		client:        client,
		fallbackStore: fallbackStore,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	response, err := c.client.GetChunkRef(ctx, &indexgatewaypb.GetChunkRefRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: allMatchers}).String(),
	})
	if err != nil {
		if isUnimplementedCallError(err) && c.fallbackStore != nil {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			return c.fallbackStore.GetChunkRefs(ctx, userID, from, through, allMatchers...)
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
		if isUnimplementedCallError(err) && c.fallbackStore != nil {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			return c.fallbackStore.GetSeries(ctx, userID, from, through, matchers...)
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
	if isUnimplementedCallError(err) && c.fallbackStore != nil {
		// Handle communication with older index gateways gracefully, by falling back to the index store calls.
		return c.fallbackStore.LabelNamesForMetricName(ctx, userID, from, through, metricName)
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
	if isUnimplementedCallError(err) && c.fallbackStore != nil {
		// Handle communication with older index gateways gracefully, by falling back to the index store calls.
		return c.fallbackStore.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
	}
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *IndexGatewayClientStore) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	resp, err := c.client.GetStats(ctx, &indexgatewaypb.IndexStatsRequest{
		From:     from,
		Through:  through,
		Matchers: (&syntax.MatchersExpr{Mts: matchers}).String(),
	})
	if err != nil {
		if isUnimplementedCallError(err) && c.fallbackStore != nil {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			// Note: this is likely a noop anyway since only
			// tsdb+ enables this and the prior index returns an
			// empty response.
			return c.fallbackStore.Stats(ctx, userID, from, through, matchers...)
		}
		return nil, err
	}

	return resp, nil
}

func (c *IndexGatewayClientStore) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	// if there is no fallback store, we can't set the chunk filterer and index gateway would take care of filtering out data
	if c.fallbackStore != nil {
		c.fallbackStore.SetChunkFilterer(chunkFilter)
	}
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
