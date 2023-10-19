package bloomgateway

import (
	"flag"

	"github.com/grafana/loki/pkg/util/ring"
)

// Config configures the Bloom Gateway component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom Gateway instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring ring.RingConfigWithRF `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom gateway servers and clients. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled is the global switch to configures whether Bloom Gateways should be used to filter chunks.
	Enabled bool `yaml:"enabled"`
	// Client configures the Bloom Gateway client
	Client ClientConfig `yaml:"client,omitempty" doc:""`
}

// RegisterFlags registers flags for the Bloom Gateway configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("bloom-gateway.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway configuration with a common prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "collectors/", f)
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Flag to enable or disable the usage of the bloom gatway component.")
	// TODO(chaudum): Figure out what the better place is for registering flags
	// -bloom-gateway.client.* or -bloom-gateway-client.*
	cfg.Client.RegisterFlags(f)
}
