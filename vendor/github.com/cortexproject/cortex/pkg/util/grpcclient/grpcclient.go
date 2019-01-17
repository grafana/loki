package grpcclient

import (
	"flag"

	"google.golang.org/grpc"
)

// Config for a gRPC client.
type Config struct {
	MaxRecvMsgSize     int  `yaml:"max_recv_msg_size"`
	MaxSendMsgSize     int  `yaml:"max_send_msg_size"`
	UseGzipCompression bool `yaml:"use_gzip_compression"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.MaxRecvMsgSize, prefix+".grpc-max-recv-msg-size", 100<<20, "gRPC client max receive message size (bytes).")
	f.IntVar(&cfg.MaxSendMsgSize, prefix+".grpc-max-send-msg-size", 16<<20, "gRPC client max send message size (bytes).")
	f.BoolVar(&cfg.UseGzipCompression, prefix+".grpc-use-gzip-compression", false, "Use compression when sending messages.")
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
func (cfg *Config) DialOption() grpc.DialOption {
	return grpc.WithDefaultCallOptions(cfg.CallOptions()...)
}
