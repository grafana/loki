package series

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/logproto"
)

type fakeClient struct {
	logproto.IndexGatewayClient
}

func (fakeClient) GetChunkRef(_ context.Context, _ *logproto.GetChunkRefRequest, _ ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	return &logproto.GetChunkRefResponse{}, nil
}

func (fakeClient) GetSeries(_ context.Context, _ *logproto.GetSeriesRequest, _ ...grpc.CallOption) (*logproto.GetSeriesResponse, error) {
	return &logproto.GetSeriesResponse{}, nil
}

func Test_IndexGatewayClient(t *testing.T) {
	idx := NewIndexGatewayClientStore(fakeClient{}, log.NewNopLogger())
	_, err := idx.GetSeries(context.Background(), "foo", model.Earliest, model.Latest)
	require.NoError(t, err)
}
