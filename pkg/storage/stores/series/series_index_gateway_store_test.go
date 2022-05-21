package series

import (
	"context"
	"log"
	"net"
	"testing"
	"time"

	"github.com/grafana/dskit/grpcclient"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
)

type fakeClient struct {
	indexgatewaypb.IndexGatewayClient
}

func (fakeClient) GetChunkRef(ctx context.Context, in *indexgatewaypb.GetChunkRefRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetChunkRefResponse, error) {
	return &indexgatewaypb.GetChunkRefResponse{}, nil
}

func (fakeClient) GetSeries(ctx context.Context, in *indexgatewaypb.GetSeriesRequest, opts ...grpc.CallOption) (*indexgatewaypb.GetSeriesResponse, error) {
	return &indexgatewaypb.GetSeriesResponse{}, nil
}

func Test_IndexGatewayClient(t *testing.T) {
	idx := IndexGatewayClientStore{
		client: fakeClient{},
		IndexStore: &indexStore{
			chunkBatchSize: 1,
		},
	}
	_, err := idx.GetSeries(context.Background(), "foo", model.Earliest, model.Latest)
	require.NoError(t, err)
}

func Test_IndexGatewayClient_Fallback(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	s := grpc.NewServer()

	// register fake grpc service with missing methods
	desc := grpc.ServiceDesc{
		ServiceName: "indexgatewaypb.IndexGateway",
		HandlerType: (*indexgatewaypb.IndexGatewayServer)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "QueryIndex",
				Handler:       nil,
				ServerStreams: true,
			},
		},
		Metadata: "pkg/storage/stores/shipper/indexgateway/indexgatewaypb/gateway.proto",
	}
	s.RegisterService(&desc, nil)

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()
	defer func() {
		s.GracefulStop()
	}()

	cfg := grpcclient.Config{
		MaxRecvMsgSize: 1024,
		MaxSendMsgSize: 1024,
	}

	dialOpts, err := cfg.DialOption(nil, nil)
	require.NoError(t, err)

	conn, err := grpc.Dial(lis.Addr().String(), dialOpts...)
	require.NoError(t, err)
	defer conn.Close()
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{From: config.DayTime{Time: model.Now().Add(-24 * time.Hour)}, Schema: "v12", RowShards: 16},
		},
	}
	schema, err := index.CreateSchema(schemaCfg.Configs[0])
	require.NoError(t, err)
	testutils.ResetMockStorage()
	tm, err := index.NewTableManager(index.TableManagerConfig{}, schemaCfg, 2*time.Hour, testutils.NewMockStorage(), nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, tm.SyncTables(context.Background()))
	idx := NewIndexGatewayClientStore(
		indexgatewaypb.NewIndexGatewayClient(conn),
		&indexStore{
			chunkBatchSize: 1,
			schema:         schema,
			schemaCfg:      schemaCfg,
			index:          testutils.NewMockStorage(),
		},
	)

	_, err = idx.GetSeries(context.Background(), "foo", model.Now(), model.Now().Add(1*time.Hour), labels.MustNewMatcher(labels.MatchEqual, "__name__", "logs"))
	require.NoError(t, err)
}
