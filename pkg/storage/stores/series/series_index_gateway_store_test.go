package series

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type fakeClient struct {
	GatewayClient
}

func (fakeClient) GetSeries(_ context.Context, _ *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	return &logproto.GetSeriesResponse{}, nil
}

func Test_IndexGatewayClient(t *testing.T) {
	idx := NewIndexGatewayClientStore(fakeClient{}, log.NewNopLogger())
	_, err := idx.GetSeries(context.Background(), "foo", model.Earliest, model.Latest)
	require.NoError(t, err)
}
