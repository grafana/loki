package indexgateway

import (
	"flag"
	"fmt"

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

// RingCfg is a wrapper for our Index Gateway ring configuration plus the replication factor.
type RingCfg struct {
	// InternalRingCfg configures the Index Gateway ring.
	loki_util.RingConfig `yaml:",inline"`

	// ReplicationFactor defines how many Index Gateway instances are assigned to each tenant.
	//
	// Whenever the store queries the ring key-value store for the Index Gateway instance responsible for tenant X,
	// multiple Index Gateway instances are expected to be returned as Index Gateway might be busy/locked for specific
	// reasons (this is assured by the spikey behavior of Index Gateway latencies).
	ReplicationFactor int `yaml:"replication_factor"`
}

// RegisterFlagsWithPrefix register all Index Gateway flags related to its ring but with a proper store prefix to avoid conflicts.
func (cfg *RingCfg) RegisterFlags(prefix, storePrefix string, f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(prefix, storePrefix, f)
	f.IntVar(&cfg.ReplicationFactor, "replication-factor", 3, "Deprecated: How many index gateway instances are assigned to each tenant. Use -index-gateway.shard-size instead. The shard size is also a per-tenant setting.")
}

// Config configures an Index Gateway server.
type Config struct {
	// Mode configures in which mode the client will be running when querying and communicating with an Index Gateway instance.
	Mode Mode `yaml:"mode"`

	// Ring configures the ring key-value store used to save and retrieve the different Index Gateway instances.
	//
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring RingCfg `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the index gateway servers and clients in case the servers are configured to run in 'ring' mode. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
}

// RegisterFlags register all IndexGatewayClientConfig flags and all the flags of its subconfigs but with a prefix (ex: shipper).
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlags("index-gateway.", "collectors/", f)
	f.StringVar((*string)(&cfg.Mode), "index-gateway.mode", SimpleMode.String(), "Defines in which mode the index gateway server will operate (default to 'simple'). It supports two modes:\n- 'simple': an index gateway server instance is responsible for handling, storing and returning requests for all indices for all tenants.\n- 'ring': an index gateway server instance is responsible for a subset of tenants instead of all tenants.")
}
