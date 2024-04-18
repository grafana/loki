package indexgateway

import (
	"flag"
	"fmt"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/util/ring"
)

const (
	NumTokens         = 128
	ReplicationFactor = 3
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

// Config configures an Index Gateway server.
type Config struct {
	// Mode configures in which mode the client will be running when querying and communicating with an Index Gateway instance.
	Mode Mode `yaml:"mode"`

	// Ring configures the ring key-value store used to save and retrieve the different Index Gateway instances.
	//
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring ring.RingConfig `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the index gateway servers and clients in case the servers are configured to run in 'ring' mode. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
}

// RegisterFlags register all IndexGatewayClientConfig flags and all the flags of its subconfigs but with a prefix (ex: shipper).
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar((*string)(&cfg.Mode), "index-gateway.mode", SimpleMode.String(), "Defines in which mode the index gateway server will operate (default to 'simple'). It supports two modes:\n- 'simple': an index gateway server instance is responsible for handling, storing and returning requests for all indices for all tenants.\n- 'ring': an index gateway server instance is responsible for a subset of tenants instead of all tenants.")

	// Ring
	skipFlags := []string{
		"index-gateway.ring.num-tokens",
		"index-gateway.ring.replication-factor",
	}
	cfg.Ring.RegisterFlagsWithPrefix("index-gateway.", "collectors/", f, skipFlags...)
	f.IntVar(&cfg.Ring.NumTokens, "index-gateway.ring.num-tokens", NumTokens, fmt.Sprintf("IGNORED: Num tokens is fixed to %d", NumTokens))
	// ReplicationFactor defines how many Index Gateway instances are assigned to each tenant.
	//
	// Whenever the store queries the ring key-value store for the Index Gateway instance responsible for tenant X,
	// multiple Index Gateway instances are expected to be returned as Index Gateway might be busy/locked for specific
	// reasons (this is assured by the spikey behavior of Index Gateway latencies).
	f.IntVar(&cfg.Ring.ReplicationFactor, "replication-factor", ReplicationFactor, "Deprecated: How many index gateway instances are assigned to each tenant. Use -index-gateway.shard-size instead. The shard size is also a per-tenant setting.")
}

func (cfg *Config) Validate() error {
	if cfg.Ring.NumTokens != NumTokens {
		return errors.New("Num tokens must not be changed as it will not take effect")
	}
	return nil
}
