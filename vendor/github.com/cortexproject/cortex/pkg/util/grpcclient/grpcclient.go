package grpcclient

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/util"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
)

// Config for a gRPC client.
type Config struct {
	MaxRecvMsgSize     int     `yaml:"max_recv_msg_size"`
	MaxSendMsgSize     int     `yaml:"max_send_msg_size"`
	UseGzipCompression bool    `yaml:"use_gzip_compression"`
	RateLimit          float64 `yaml:"rate_limit"`
	RateLimitBurst     int     `yaml:"rate_limit_burst"`

	BackoffOnRatelimits bool               `yaml:"backoff_on_ratelimits"`
	BackoffConfig       util.BackoffConfig `yaml:"backoff_config"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.MaxRecvMsgSize, prefix+".grpc-max-recv-msg-size", 100<<20, "gRPC client max receive message size (bytes).")
	f.IntVar(&cfg.MaxSendMsgSize, prefix+".grpc-max-send-msg-size", 16<<20, "gRPC client max send message size (bytes).")
	f.BoolVar(&cfg.UseGzipCompression, prefix+".grpc-use-gzip-compression", false, "Use compression when sending messages.")
	f.Float64Var(&cfg.RateLimit, prefix+".grpc-client-rate-limit", 0., "Rate limit for gRPC client; 0 means disabled.")
	f.IntVar(&cfg.RateLimitBurst, prefix+".grpc-client-rate-limit-burst", 0, "Rate limit burst for gRPC client.")
	f.BoolVar(&cfg.BackoffOnRatelimits, prefix+".backoff-on-ratelimits", false, "Enable backoff and retry when we hit ratelimits.")

	cfg.BackoffConfig.RegisterFlags(prefix, f)
}

// CallOptions returns the config in terms of CallOptions.
func (cfg *Config) CallOptions() []grpc.CallOption {
	var opts []grpc.CallOption
	opts = append(opts, grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize))
	opts = append(opts, grpc.MaxCallSendMsgSize(cfg.MaxSendMsgSize))
	if cfg.UseGzipCompression {
		opts = append(opts, grpc.UseCompressor("gzip"))
	}
	return opts
}

// DialOption returns the config as a grpc.DialOptions.
func (cfg *Config) DialOption(unaryClientInterceptors []grpc.UnaryClientInterceptor, streamClientInterceptors []grpc.StreamClientInterceptor) []grpc.DialOption {
	if cfg.BackoffOnRatelimits {
		unaryClientInterceptors = append([]grpc.UnaryClientInterceptor{NewBackoffRetry(cfg.BackoffConfig)}, unaryClientInterceptors...)
	}

	if cfg.RateLimit > 0 {
		unaryClientInterceptors = append([]grpc.UnaryClientInterceptor{NewRateLimiter(cfg)}, unaryClientInterceptors...)
	}

	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(cfg.CallOptions()...),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(unaryClientInterceptors...)),
		grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(streamClientInterceptors...)),
	}
}
