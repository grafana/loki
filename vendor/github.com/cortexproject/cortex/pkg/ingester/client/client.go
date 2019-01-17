package client

import (
	"flag"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // get gzip compressor registered
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	cortex_middleware "github.com/cortexproject/cortex/pkg/util/middleware"
	"github.com/weaveworks/common/middleware"
)

var ingesterClientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "ingester_client_request_duration_seconds",
	Help:      "Time spent doing Ingester requests.",
	Buckets:   prometheus.ExponentialBuckets(0.001, 4, 6),
}, []string{"operation", "status_code"})

// HealthAndIngesterClient is the union of IngesterClient and grpc_health_v1.HealthClient.
type HealthAndIngesterClient interface {
	IngesterClient
	grpc_health_v1.HealthClient
}

type closableHealthAndIngesterClient struct {
	IngesterClient
	grpc_health_v1.HealthClient
	conn *grpc.ClientConn
}

// MakeIngesterClient makes a new IngesterClient
func MakeIngesterClient(addr string, cfg Config) (HealthAndIngesterClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
			cortex_middleware.PrometheusGRPCUnaryInstrumentation(ingesterClientRequestDuration),
		)),
		grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientUserHeaderInterceptor,
			cortex_middleware.PrometheusGRPCStreamInstrumentation(ingesterClientRequestDuration),
		)),
		cfg.GRPCClientConfig.DialOption(),
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &closableHealthAndIngesterClient{
		IngesterClient: NewIngesterClient(conn),
		HealthClient:   grpc_health_v1.NewHealthClient(conn),
		conn:           conn,
	}, nil
}

func (c *closableHealthAndIngesterClient) Close() error {
	return c.conn.Close()
}

// Config is the configuration struct for the ingester client
type Config struct {
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

// RegisterFlags registers configuration settings used by the ingester client config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlags("ingester.client", f)

	// Deprecated.
	f.Int("ingester.client.max-recv-message-size", 64*1024*1024, "DEPRECATED. Maximum message size, in bytes, this client will receive.")
	f.Bool("ingester.client.compress-to-ingester", false, "DEPRECATED. Compress data in calls to ingesters.")
	f.Bool("distributor.compress-to-ingester", false, "DEPRECATED. Compress data in calls to ingesters. (DEPRECATED: use ingester.client.compress-to-ingester instead")
}
