package client

import (
	"flag"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // get gzip compressor registered
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/util/grpcclient"
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
	Close() error
}

type closableHealthAndIngesterClient struct {
	IngesterClient
	grpc_health_v1.HealthClient
	conn *grpc.ClientConn
}

// MakeIngesterClient makes a new IngesterClient
func MakeIngesterClient(addr string, cfg Config) (HealthAndIngesterClient, error) {
	opts := []grpc.DialOption{grpc.WithInsecure()}
	opts = append(opts, cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(ingesterClientRequestDuration))...)
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
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("ingester.client", f)
}
