package shipper

import (
	"fmt"
	"io"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type IndexGatewayGRPCPool struct {
	grpc_health_v1.HealthClient
	indexgatewaypb.IndexGatewayClient
	io.Closer
}

func NewIndexGatewayGRPCPool(address string, opts []grpc.DialOption) (*IndexGatewayGRPCPool, error) {
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("shipper new grpc pool dial: %w", err)
	}

	return &IndexGatewayGRPCPool{
		Closer:             conn,
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		IndexGatewayClient: indexgatewaypb.NewIndexGatewayClient(conn),
	}, nil
}
