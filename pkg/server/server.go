package server

import (
	"flag"
	"time"

	"github.com/weaveworks/common/server"
)

// CommonConfig extends weaveworks server config
type CommonConfig struct {
	server.Config               `yaml:",inline"`
	GRPCServerConnectionTimeout time.Duration `yaml:"grpc_server_connection_timeout"`
}

// RegisterFlags add internal server flags to flagset
func (cfg *CommonConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	f.DurationVar(&cfg.GRPCServerConnectionTimeout, "server.grpc.connection.timeout", time.Minute*2, "sets the timeout for connection establishment (up to and including HTTP/2 handshaking) for all new connections.  If this is not set, the default is 120 seconds.  A zero or negative value will result in an immediate timeout.")
}
