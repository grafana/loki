package series

import (
	"context"
	"fmt"

	"github.com/gogo/status"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

type IndexGatewayClientStore struct {
	client logproto.IndexGatewayClient
	// fallbackStore is used only to keep index gateways backwards compatible.
	// Previously index gateways would only serve index rows from boltdb-shipper files.
	// tsdb also supports configuring index gateways but there is no concept of serving index rows so
	// the fallbackStore could be nil and should be checked before use
	fallbackStore index.Reader
}

func NewIndexGatewayClientStore(client logproto.IndexGatewayClient, fallbackStore index.Reader) index.ReaderWriter {
	return &IndexGatewayClientStore{
		client:        client,
		fallbackStore: fallbackStore,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	response, err := c.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{
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
	resp, err := c.client.GetSeries(ctx, &logproto.GetSeriesRequest{
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
	resp, err := c.client.LabelNamesForMetricName(ctx, &logproto.LabelNamesForMetricNameRequest{
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
	resp, err := c.client.LabelValuesForMetricName(ctx, &logproto.LabelValuesForMetricNameRequest{
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
	resp, err := c.client.GetStats(ctx, &logproto.IndexStatsRequest{
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

func (c *IndexGatewayClientStore) SeriesVolume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	resp, err := c.client.GetSeriesVolume(ctx, &logproto.VolumeRequest{
		From:         from,
		Through:      through,
		Matchers:     (&syntax.MatchersExpr{Mts: matchers}).String(),
		Limit:        limit,
		TargetLabels: targetLabels,
	})
	if err != nil {
		if isUnimplementedCallError(err) && c.fallbackStore != nil {
			// Handle communication with older index gateways gracefully, by falling back to the index store calls.
			// Note: this is likely a noop anyway since only
			// tsdb+ enables this and the prior index returns an
			// empty response.
			return c.fallbackStore.SeriesVolume(ctx, userID, from, through, limit, targetLabels, matchers...)
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

func (c *IndexGatewayClientStore) IndexChunk(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return fmt.Errorf("index writes not supported on index gateway client")
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
