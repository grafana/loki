package metastoreclient

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/services"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

type Config struct {
	MetastoreAddress string            `yaml:"address"`
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config" doc:"description=Configures the gRPC client used to communicate with the metastore."`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.MetastoreAddress, "metastore.address", "localhost:9095", "")
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("metastore.grpc-client-config", f)
}

func (cfg *Config) Validate() error {
	if cfg.MetastoreAddress == "" {
		return fmt.Errorf("metastore.address is required")
	}
	return cfg.GRPCClientConfig.Validate()
}

type Client struct {
	metastorepb.MetastoreServiceClient
	service services.Service
	conn    *grpc.ClientConn
	config  Config
}

func New(config Config, r prometheus.Registerer) (c *Client, err error) {
	c = &Client{config: config}
	c.conn, err = dial(c.config, r)
	if err != nil {
		return nil, err
	}
	c.MetastoreServiceClient = metastorepb.NewMetastoreServiceClient(c.conn)
	c.service = services.NewIdleService(nil, c.stopping)
	return c, nil
}

func (c *Client) stopping(error) error { return c.conn.Close() }

func (c *Client) Service() services.Service { return c.service }

func dial(cfg Config, r prometheus.Registerer) (*grpc.ClientConn, error) {
	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:                        "loki_metastore_request_duration_seconds",
		Help:                        "Time (in seconds) spent serving requests when using the metastore",
		Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
		NativeHistogramBucketFactor: 1.1,
	}, []string{"operation", "status_code"})
	if r != nil {
		err := r.Register(latency)
		if err != nil {
			alreadyErr, ok := err.(prometheus.AlreadyRegisteredError)
			if !ok {
				return nil, err
			}
			latency = alreadyErr.ExistingCollector.(*prometheus.HistogramVec)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	options, err := cfg.GRPCClientConfig.DialOption(instrumentation(latency))
	if err != nil {
		return nil, err
	}
	// TODO: https://github.com/grpc/grpc-proto/blob/master/grpc/service_config/service_config.proto
	options = append(options, grpc.WithDefaultServiceConfig(grpcServiceConfig))

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	return grpc.Dial(cfg.MetastoreAddress, options...)
}

const grpcServiceConfig = `{
	"healthCheckConfig": {
		"serviceName": "metastorepb.MetastoreService.RaftLeader"
	},
	"loadBalancingPolicy":"round_robin",
    "methodConfig": [{
        "name": [{"service": "metastorepb.MetastoreService"}],
        "waitForReady": true,
        "retryPolicy": {
            "MaxAttempts": 4,
            "InitialBackoff": ".01s",
            "MaxBackoff": ".01s",
            "BackoffMultiplier": 1.0,
            "RetryableStatusCodes": [ "UNAVAILABLE" ]
        }
    }]
}`

func instrumentation(latency *prometheus.HistogramVec) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var unaryInterceptors []grpc.UnaryClientInterceptor
	unaryInterceptors = append(unaryInterceptors, otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()))
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(latency))

	var streamInterceptors []grpc.StreamClientInterceptor
	streamInterceptors = append(streamInterceptors, otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()))
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(latency))

	return unaryInterceptors, streamInterceptors
}
