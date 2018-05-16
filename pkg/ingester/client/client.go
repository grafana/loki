package client

import (
	"flag"
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

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxRecvMsgSize, "ingester.client.max-recv-message-size", 64*1024*1024, "Maximum message size, in bytes, this client will receive.")
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
		logproto.PusherClient
		logproto.QuerierClient
		io.Closer
	}{
		PusherClient:  logproto.NewPusherClient(conn),
		QuerierClient: logproto.NewQuerierClient(conn),
		Closer:        conn,
	}, nil
}
