package frontend

import (
	"flag"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/server"
)

var (
	limitsClients = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "loki_ingest_limits_clients",
		Help: "The current number of ingest limits clients.",
	})
	limitsClientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loki_ingest_limits_client_request_duration_seconds",
		Help:    "Time spent doing ingest limits requests.",
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
	}, []string{"operation", "status_code"})
)

// ClientConfig contains the config for an ingest-limits client.
type ClientConfig struct {
	GRPCClientConfig             grpcclient.Config              `yaml:"grpc_client_config" doc:"description=Configures client gRPC connections to limits service."`
	PoolConfig                   PoolConfig                     `yaml:"pool_config,omitempty" doc:"description=Configures client gRPC connections pool to limits service."`
	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`
}

func (cfg *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".limits-client", f)
	cfg.PoolConfig.RegisterFlagsWithPrefix(prefix, f)
}

// PoolConfig contains the config for a pool of ingest-limits clients.
type PoolConfig struct {
	ClientCleanupPeriod     time.Duration `yaml:"client_cleanup_period"`
	HealthCheckIngestLimits bool          `yaml:"health_check_ingest_limits"`
	RemoteTimeout           time.Duration `yaml:"remote_timeout"`
}

func (cfg *PoolConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.ClientCleanupPeriod, prefix+".client-cleanup-period", 15*time.Second, "How frequently to clean up clients for ingest-limits that have gone away.")
	f.BoolVar(&cfg.HealthCheckIngestLimits, prefix+".health-check-ingest-limits", true, "Run a health check on each ingest-limits client during periodic cleanup.")
	f.DurationVar(&cfg.RemoteTimeout, prefix+".remote-timeout", 1*time.Second, "Timeout for the health check.")
}

// IngestLimitsClient is a gRPC client for the ingest-limits backend.
// The ingest limits service uses a pool of clients to get usage data from
// individual backends.
type IngestLimitsClient struct {
	logproto.IngestLimitsClient
	grpc_health_v1.HealthClient
	io.Closer
}

// NewIngestLimitsClient returns a new IngestLimitsClient for the specified
// ingest-limits backend.
func NewIngestLimitsClient(cfg ClientConfig, addr string) (*IngestLimitsClient, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}
	dialOpts, err := cfg.GRPCClientConfig.DialOption(getGRPCInterceptors(&cfg))
	if err != nil {
		return nil, err
	}
	opts = append(opts, dialOpts...)
	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &IngestLimitsClient{
		IngestLimitsClient: logproto.NewIngestLimitsClient(conn),
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		Closer:             conn,
	}, nil
}

// getInterceptors returns the gRPC interceptors for the given ClientConfig.
func getGRPCInterceptors(cfg *ClientConfig) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var (
		unaryInterceptors  []grpc.UnaryClientInterceptor
		streamInterceptors []grpc.StreamClientInterceptor
	)

	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors, server.UnaryClientQueryTagsInterceptor)
	unaryInterceptors = append(unaryInterceptors, server.UnaryClientHTTPHeadersInterceptor)
	unaryInterceptors = append(unaryInterceptors, otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()))
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(limitsClientRequestDuration))

	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors, server.StreamClientQueryTagsInterceptor)
	streamInterceptors = append(streamInterceptors, server.StreamClientHTTPHeadersInterceptor)
	streamInterceptors = append(streamInterceptors, otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()))
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(limitsClientRequestDuration))

	return unaryInterceptors, streamInterceptors
}

// NewIngestLimitsClientPool returns a new pool of IngestLimitsClients.
func NewIngestLimitsClientPool(
	name string,
	cfg PoolConfig,
	ring ring.ReadRing,
	factory ring_client.PoolFactory,
	logger log.Logger,
) *ring_client.Pool {
	poolCfg := ring_client.PoolConfig{
		CheckInterval:      cfg.ClientCleanupPeriod,
		HealthCheckEnabled: cfg.HealthCheckIngestLimits,
		HealthCheckTimeout: cfg.RemoteTimeout,
	}
	return ring_client.NewPool(name, poolCfg, ring_client.NewRingServiceDiscovery(ring), factory, limitsClients, logger)
}
