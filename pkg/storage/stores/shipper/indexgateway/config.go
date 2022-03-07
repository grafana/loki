package indexgateway

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/grpcclient"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	loki_util "github.com/grafana/loki/pkg/util"
)

// Mode represents in which mode an Index Gateway instance is running.
//
// Right now, two modes are supported: simple mode (default) and ring mode.
type Mode string

// Set implements a flag interface, and is necessary to use the IndexGatewayClientMode as a flag.
func (i Mode) Set(v string) error {
	switch v {
	case string(SimpleMode):
		// nolint:ineffassign
		i = SimpleMode
	case string(RingMode):
		// nolint:ineffassign
		i = RingMode
	default:
		return fmt.Errorf("mode %s not supported. list of supported modes: simple (default), ring", v)
	}
	return nil
}

// String implements a flag interface, and is necessary to use the IndexGatewayClientMode as a flag.
func (i Mode) String() string {
	switch i {
	case RingMode:
		return string(RingMode)
	default:
		return string(SimpleMode)
	}
}

const (
	// SimpleMode is a mode where an Index Gateway instance solely handle all the work.
	SimpleMode Mode = "simple"

	// RingMode is a mode where different Index Gateway instances are assigned to handle different tenants.
	//
	// It is more horizontally scalable than the simple mode, but requires running a key-value store ring.
	RingMode Mode = "ring"
)

// SimpleModeConfig configures an Index Gateway in the simple mode, where a single instance is assigned all tenants.
//
// If the Index Gateway is running in ring mode this configuration shall be ignored.
type SimpleModeConfig struct {
	// Address of the Index Gateway instance responsible for retaining the index for all tenants.
	Address string `yaml:"server_address,omitempty"`
}

// RegisterFlagsWithPrefix register all SimpleModeConfig flags and all the flags of its subconfigs but with a prefix.
func (cfg *SimpleModeConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server running in simple mode.")
}

// RingModeConfig configures an Index Gateway in the ring mode, where an instance is responsible for a subset of tenants.
//
// If the Index Gateway is running in simple mode this configuration shall be ignored.
type RingModeConfig struct {
	// Ring configures the ring key-value store used to save and retrieve the different Index Gateway instances.
	//
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring loki_util.RingConfig `yaml:"index_gateway_ring,omitempty"` // TODO: maybe just `yaml:"ring"`?

	// PoolConfig configures a pool of GRPC connections to the different Index Gateway instances.
	PoolConfig clientpool.PoolConfig `yaml:"pool_config"`

	// GRPCClientConfig configures GRPC parameters used in the connection between a component and the Index Gateway.
	//
	// It is used by both, simple and ring mode.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

// RegisterFlagsWithPrefix register all RingModeConfig flags and all the flags of its subconfigs but with a prefix.
func (cfg *RingModeConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix+".", "collectors/", f)
}

// Config configures an Index Gateway used by the different components.
//
// If the mode is set to simple (default), only the SimpleModeConfig is relevant. Otherwise, for the ring mode,
// only the RingModeConfig is relevant.
type Config struct {
	// Mode configures in which mode the client will be running when querying and communicating with an Index Gateway instance.
	Mode Mode `yaml:"mode"` // TODO: ring, simple

	// RingMode configures the client to communicate with Index Gateway instances in the ring mode.
	RingModeConfig `yaml:",inline"`

	// SimpleModeConfig configures the client to communicate with Index Gateway instances in the simple mode.
	SimpleModeConfig `yaml:",inline"`
}

// RegisterFlags register all IndexGatewayClientConfig flags and all the flags of its subconfigs but with a prefix (ex: shipper).
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Mode = SimpleMode // default mode.
	cfg.RingModeConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.SimpleModeConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)

	f.Var(cfg.Mode, prefix+".mode", "Mode to run the index gateway client. Supported modes are simple (default) and ring.")
}
