package series

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type IndexGatewayClientStore struct {
	client IndexGatewayClient
	*IndexStore
}

type IndexGatewayClient interface {
	GetChunkRef(ctx context.Context, in *indexgatewaypb.GetChunkRefRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetChunkRefResponse, error)
	LabelNamesForMetricName(ctx context.Context, in *indexgatewaypb.LabelNamesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
	LabelValuesForMetricName(ctx context.Context, in *indexgatewaypb.LabelValuesForMetricNameRequest, opts ...grpc.CallOption) (*indexgatewaypb.LabelResponse, error)
}

func NewIndexGatewayClientStore(client IndexGatewayClient, index *IndexStore) *IndexGatewayClientStore {
	return &IndexGatewayClientStore{
		client:     client,
		IndexStore: index,
	}
}

func (c *IndexGatewayClientStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	return c.IndexStore.GetChunkRefs(ctx, userID, from, through, allMatchers...)
}

func (c *IndexGatewayClientStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	return c.GetSeries(ctx, userID, from, through, matchers...)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *IndexGatewayClientStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	return c.IndexStore.LabelNamesForMetricName(ctx, userID, from, through, metricName)
}

func (c *IndexGatewayClientStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	return c.IndexStore.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName)
}
