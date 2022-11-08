package gatewayclient

import (
	"io"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/logproto"
)

// IndexGatewayGRPCPool represents a pool of gRPC connections to different index gateway instances.
//
// Only used when Index Gateway is configured to run in ring mode.
type IndexGatewayGRPCPool struct {
	grpc_health_v1.HealthClient
	logproto.IndexGatewayClient
	io.Closer
}

// NewIndexGatewayGRPCPool instantiates a new pool of IndexGateway GRPC connections.
//
// Internally, it also instantiates a protobuf index gateway client and a health client.
func NewIndexGatewayGRPCPool(address string, opts []grpc.DialOption) (*IndexGatewayGRPCPool, error) {
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "shipper new grpc pool dial")
	}

	return &IndexGatewayGRPCPool{
		Closer:             conn,
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		IndexGatewayClient: logproto.NewIndexGatewayClient(conn),
	}, nil
}
