package indexgateway

import (
	"io"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// ClientPool represents a pool of gRPC connections to different index gateway instances.
//
// Only used when Index Gateway is configured to run in ring mode.
type ClientPool struct {
	grpc_health_v1.HealthClient
	logproto.IndexGatewayClient
	io.Closer
}

// NewClientPool instantiates a new pool of IndexGateway GRPC connections.
//
// Internally, it also instantiates a protobuf index gateway client and a health client.
func NewClientPool(address string, opts []grpc.DialOption) (*ClientPool, error) {
	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "shipper new grpc pool dial")
	}

	return &ClientPool{
		Closer:             conn,
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		IndexGatewayClient: logproto.NewIndexGatewayClient(conn),
	}, nil
}
