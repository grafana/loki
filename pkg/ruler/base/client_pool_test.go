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
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
)

func Test_newRulerClientFactory(t *testing.T) {
	// Create a GRPC server used to query the mocked service.
	grpcServer := grpc.NewServer()
	defer grpcServer.GracefulStop()

	srv := &mockRulerServer{}
	RegisterRulerServer(grpcServer, srv)

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
	factory := newRulerClientFactory(cfg, reg)

	for i := 0; i < 2; i++ {
		client, err := factory(listener.Addr().String())
		require.NoError(t, err)
		defer client.Close() //nolint:errcheck

		ctx := user.InjectOrgID(context.Background(), "test")
		_, err = client.(*rulerExtendedClient).Rules(ctx, &RulesRequest{})
		assert.NoError(t, err)
	}

	// Assert on the request duration metric, but since it's a duration histogram and
	// we can't predict the exact time it took, we need to workaround it.
	metrics, err := reg.Gather()
	require.NoError(t, err)

	assert.Len(t, metrics, 1)
	assert.Equal(t, "cortex_ruler_client_request_duration_seconds", metrics[0].GetName())
	assert.Equal(t, dto.MetricType_HISTOGRAM, metrics[0].GetType())
	assert.Len(t, metrics[0].GetMetric(), 1)
	assert.Equal(t, uint64(2), metrics[0].GetMetric()[0].GetHistogram().GetSampleCount())
}

type mockRulerServer struct{}

func (m *mockRulerServer) Rules(context.Context, *RulesRequest) (*RulesResponse, error) {
	return &RulesResponse{}, nil
}
