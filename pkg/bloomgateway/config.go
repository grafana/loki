package bloomgateway

import (
	"flag"

	"github.com/grafana/loki/pkg/util"
)

// RingCfg is a wrapper for our internally used ring configuration plus the replication factor.
type RingCfg struct {
	// RingConfig configures the Bloom Gateway ring.
	util.RingConfig `yaml:",inline"`
	// ReplicationFactor defines how many replicas of the Bloom Gateway store a single data shard.
	ReplicationFactor int `yaml:"replication_factor"`
}

// RegisterFlagsWithPrefix registers all Bloom Gateway CLI flags.
func (cfg *RingCfg) RegisterFlagsWithPrefix(prefix, storePrefix string, f *flag.FlagSet) {
	cfg.RingConfig.RegisterFlagsWithPrefix(prefix, storePrefix, f)
	f.IntVar(&cfg.ReplicationFactor, prefix+"replication-factor", 3, "Factor for data replication on the bloom gateways.")
}

// Config configures the Bloom Gateway component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom Gateway instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring RingCfg `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom gateway servers and clients. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled configures whether bloom gateways should be used to filter chunks.
	Enabled bool `yaml:"enabled"`
}

// RegisterFlags registers flags for the Bloom Gateway configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("bloom-gateway.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway configuration with a common prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "collectors/", f)
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Flag to enable or disable the usage of the bloom gatway component.")
}
