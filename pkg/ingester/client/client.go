package client

import (
	"io"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mwitkow/go-grpc-middleware"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type Config struct {
	MaxRecvMsgSize int
}

func New(cfg Config, addr string) (grpc_health_v1.HealthClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		)),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize),
			grpc.UseCompressor("gzip"),
		),
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return struct {
		grpc_health_v1.HealthClient
		io.Closer
	}{
		HealthClient: logproto.NewAggregatorClient(conn),
		Closer:       conn,
	}, nil
}
