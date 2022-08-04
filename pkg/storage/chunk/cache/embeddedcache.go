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
	MaxItems    int           `yaml:"max_items"` // TODO(kavi): should we stop supporting it?
	TTL         time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `DefaultPurgeInterval`
	PurgeInterval time.Duration `yaml:"-"`

	// singleton configs(irrespective of what caches uses it). Mainly useful for distributed caches.
	globalConfig EmbeddedcacheSingletonConfig `yaml:"-"`
}

func (cfg *EmbeddedcacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"embedded-cache.enabled", false, description+"Whether embedded cache is enabled.")
	f.BoolVar(&cfg.Distributed, prefix+"embedded-cache.distributed", false, description+"Whether embedded cache is enabled with distributed mode.")
	f.Int64Var(&cfg.MaxSizeMB, prefix+"embedded-cache.max-size-mb", 100, description+"Maximum memory size of the cache in MB.")
	f.IntVar(&cfg.MaxItems, prefix+"embedded-cache.max-items", 0, description+"Maximum number of entries in the cache.")
	f.DurationVar(&cfg.TTL, prefix+"embedded-cache.ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")

}

func (cfg *EmbeddedcacheConfig) IsEnabledWithDistributed() bool {
	return cfg.Enabled && cfg.Distributed
}

func (cfg *EmbeddedcacheConfig) IsEnabledWithoutDistributed() bool {
	return cfg.Enabled && !cfg.Distributed
}

type EmbeddedcacheSingletonConfig struct {
	// distributed cache configs. Have no meaning if `Distributed=false`.
	ListenPort int     `yaml:"listen_port,omitempty"`
	Ring       RingCfg `yaml:"ring,omitempty"`

	// Default capacity if none provided while creating each "Group".
	MaxSizeMB int64 `yaml:"max_size__mb,omitempty"`
}

func (cfg *EmbeddedcacheSingletonConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.IntVar(&cfg.ListenPort, prefix+"embedded-cache.listen_port", 4100, "The port to use for groupcache communication")
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "", f)
	f.Int64Var(&cfg.MaxSizeMB, prefix+".max-size-mb", 100, "Maximum memory size of the cache in MB.")
}
