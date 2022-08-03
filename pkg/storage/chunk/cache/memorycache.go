package cache

import (
	"flag"
	"time"
)

const (
	DefaultPurgeInterval = 1 * time.Minute
)

// EmbeddedcacheConfig represents in-process embedded cache config.
// It can also be distributed, sharding keys across peers when run with microservices
// or SSD mode.
type EmbeddedcacheConfig struct {
	Distributed bool          `yaml:"distributed,omitempty"`
	Enabled     bool          `yaml:"enabled,omitempty"`
	MaxSizeMB   int64         `yaml:"max_size_mb"`
	MaxItems    int           `yaml:"max_items"`
	TTL         time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `DefaultPurgeInterval`
	PurgeInterval time.Duration

	// distributed cache configs. Have no meaning if `Distributed=false`.
	Ring       RingCfg `yaml:"ring,omitempty"`
	ListenPort int     `yaml:"listen_port,omitempty"`
}

func (cfg *EmbeddedcacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.Int64Var(&cfg.MaxSizeMB, prefix+".max-size-mb", 100, description+"Maximum memory size of the cache in MB.")
	f.IntVar(&cfg.MaxItems, prefix+".max-items", 0, description+"Maximum number of entries in the cache.")
	f.DurationVar(&cfg.TTL, prefix+".ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "", f)
	f.IntVar(&cfg.ListenPort, prefix+".listen_port", 4100, "The port to use for groupcache communication")
}

func (em EmbeddedcacheConfig) IsEnabledWithDistributed() bool {
	return em.Enabled && em.Distributed
}

func (em EmbeddedcacheConfig) IsEnabledWithoutDistributed() bool {
	return em.Enabled && !em.Distributed
}
