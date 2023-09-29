package bloomcompactor

import (
	"flag"

	"github.com/grafana/loki/pkg/util"
)

// Config configures the bloom-compactor component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom-Compactor instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring RingCfg `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom-compactor servers. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled configures whether bloom-compactors should be used to compact index values into bloomfilters
	Enabled bool `yaml:"enabled"`
}

// RegisterFlags registers flags for the Bloom-Compactor configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlags("bloom-compactor.", "collectors/", f)
	f.BoolVar(&cfg.Enabled, "bloom-compactor.enabled", false, "Flag to enable or disable the usage of the bloom-compactor component.")
}

// RingCfg is a wrapper for our internally used ring configuration plus the replication factor.
type RingCfg struct {
	// RingConfig configures the Bloom-Compactor ring.
	util.RingConfig `yaml:",inline"`
	// ReplicationFactor defines how many replicas of the Bloom-Compactor store the given range of streams.
	ReplicationFactor int `yaml:"replication_factor"`
}

func (cfg *RingCfg) RegisterFlags(prefix, storePrefix string, f *flag.FlagSet) {
	cfg.RingConfig.RegisterFlagsWithPrefix(prefix, storePrefix, f)
	f.IntVar(&cfg.ReplicationFactor, prefix+"replication-factor", 1, "Factor for data replication on the bloom compactors")
}
