package series

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type fakeClient struct {
	indexgatewaypb.IndexGatewayClient
}

func (fakeClient) GetChunkRef(ctx context.Context, in *indexgatewaypb.GetChunkRefRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetChunkRefResponse, error) {
	return &indexgatewaypb.GetChunkRefResponse{}, nil
}

func Test_IndexGatewayClient(t *testing.T) {
	idx := IndexGatewayClientStore{
		client: fakeClient{},
		IndexStore: &IndexStore{
			chunkBatchSize: 1,
		},
	}
	_, err := idx.GetSeries(context.Background(), "foo", model.Earliest, model.Latest)
	require.NoError(t, err)
}
