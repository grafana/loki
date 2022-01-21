package base

import (
	"context"
	"net"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storegateway/storegatewaypb"
)

func Test_newStoreGatewayClientFactory(t *testing.T) {
	// Create a GRPC server used to query the mocked service.
	grpcServer := grpc.NewServer()
	defer grpcServer.GracefulStop()

	srv := &mockStoreGatewayServer{}
	storegatewaypb.RegisterStoreGatewayServer(grpcServer, srv)

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	go func() {
		require.NoError(t, grpcServer.Serve(listener))
	}()

	// Create a client factory and query back the mocked service
	// with different clients.
	cfg := grpcclient.Config{}
	flagext.DefaultValues(&cfg)

	reg := prometheus.NewPedanticRegistry()
	factory := newStoreGatewayClientFactory(cfg, reg)

	for i := 0; i < 2; i++ {
		client, err := factory(listener.Addr().String())
		require.NoError(t, err)
		defer client.Close() //nolint:errcheck

		ctx := user.InjectOrgID(context.Background(), "test")
		stream, err := client.(*storeGatewayClient).Series(ctx, &storepb.SeriesRequest{})
		assert.NoError(t, err)

		// Read the entire response from the stream.
		for _, err = stream.Recv(); err == nil; {
		}
	}

	// Assert on the request duration metric, but since it's a duration histogram and
	// we can't predict the exact time it took, we need to workaround it.
	metrics, err := reg.Gather()
	require.NoError(t, err)

	assert.Len(t, metrics, 1)
	assert.Equal(t, "cortex_storegateway_client_request_duration_seconds", metrics[0].GetName())
	assert.Equal(t, dto.MetricType_HISTOGRAM, metrics[0].GetType())
	assert.Len(t, metrics[0].GetMetric(), 1)
	assert.Equal(t, uint64(2), metrics[0].GetMetric()[0].GetHistogram().GetSampleCount())
}

type mockStoreGatewayServer struct{}

func (m *mockStoreGatewayServer) Series(_ *storepb.SeriesRequest, srv storegatewaypb.StoreGateway_SeriesServer) error {
	return nil
}

func (m *mockStoreGatewayServer) LabelNames(context.Context, *storepb.LabelNamesRequest) (*storepb.LabelNamesResponse, error) {
	return nil, nil
}

func (m *mockStoreGatewayServer) LabelValues(context.Context, *storepb.LabelValuesRequest) (*storepb.LabelValuesResponse, error) {
	return nil, nil
}
