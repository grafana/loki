// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package client

import (
	"flag"
	"io"

	"github.com/cortexproject/cortex/pkg/util/grpcclient"

	"github.com/grafana/agent/pkg/agentproto"
	"github.com/grafana/agent/pkg/util"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
)

// ScrapingServiceClient wraps agentproto.ScrapingServiceClient with a Close method.
type ScrapingServiceClient interface {
	agentproto.ScrapingServiceClient
	io.Closer
}

var (
	// DefaultConfig provides default Config values.
	DefaultConfig = *util.DefaultConfigFromFlags(&Config{}).(*Config)
)

// Config controls how scraping service clients are created.
type Config struct {
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	type plain Config
	return unmarshal((*plain)(c))
}

// RegisterFlags registers flags to the provided flag set.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("prometheus.", f)
}

// RegisterFlagsWithPrefix registers flags to the provided flag set with the
// specified prefix.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+"service-client", f)
}

// New returns a new scraping service client.
func New(cfg Config, addr string) (ScrapingServiceClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}
	grpcDialOpts, err := cfg.GRPCClientConfig.DialOption(instrumentation())
	if err != nil {
		return nil, err
	}
	opts = append(opts, grpcDialOpts...)
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}

	return struct {
		agentproto.ScrapingServiceClient
		io.Closer
	}{
		ScrapingServiceClient: agentproto.NewScrapingServiceClient(conn),
		Closer:                conn,
	}, nil
}

func instrumentation() ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	unary := []grpc.UnaryClientInterceptor{
		otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
		middleware.ClientUserHeaderInterceptor,
	}
	stream := []grpc.StreamClientInterceptor{
		otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
		middleware.StreamClientUserHeaderInterceptor,
	}
	return unary, stream
}
