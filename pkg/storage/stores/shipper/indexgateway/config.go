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

// Config configures an Index Gateway server.
type Config struct {
	// Mode configures in which mode the client will be running when querying and communicating with an Index Gateway instance.
	Mode Mode `yaml:"mode"`

	// Ring configures the ring key-value store used to save and retrieve the different Index Gateway instances.
	//
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring loki_util.RingConfig `yaml:"ring,omitempty"`
}

// RegisterFlags register all IndexGatewayClientConfig flags and all the flags of its subconfigs but with a prefix (ex: shipper).
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix+".", "collectors/", f)
	f.StringVar((*string)(&cfg.Mode), prefix+".mode", SimpleMode.String(), "mode in which the index gateway client will be running")
}
