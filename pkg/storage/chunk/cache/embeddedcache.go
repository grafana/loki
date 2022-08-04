package cache

import (
	"flag"
	"time"
)

const (
	DefaultPurgeInterval = 1 * time.Minute
)

// EmbeddedCacheConfig represents in-process embedded cache config.
// It can also be distributed, sharding keys across peers when run with microservices
// or SSD mode.
type EmbeddedCacheConfig struct {
	Distributed bool          `yaml:"distributed,omitempty"`
	Enabled     bool          `yaml:"enabled,omitempty"`
	MaxSizeMB   int64         `yaml:"max_size_mb"`
	TTL         time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `DefaultPurgeInterval`
	PurgeInterval time.Duration `yaml:"-"`
}

func (cfg *EmbeddedCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"embedded-cache.enabled", false, description+"Whether embedded cache is enabled.")
	f.BoolVar(&cfg.Distributed, prefix+"embedded-cache.distributed", false, description+"Whether embedded cache is enabled with distributed mode.")
	f.Int64Var(&cfg.MaxSizeMB, prefix+"embedded-cache.max-size-mb", 100, description+"Maximum memory size of the cache in MB.")
	f.DurationVar(&cfg.TTL, prefix+"embedded-cache.ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")

}

func (cfg *EmbeddedCacheConfig) IsEnabledWithDistributed() bool {
	return cfg.Enabled && cfg.Distributed
}

func (cfg *EmbeddedCacheConfig) IsEnabledWithoutDistributed() bool {
	return cfg.Enabled && !cfg.Distributed
}

// EmbeddedCacheSingletonConfig defines global singleton needed by Embedded cache(particularly used in distributed fashion)
type EmbeddedCacheSingletonConfig struct {
	// distributed cache configs. Have no meaning if `Distributed=false`.
	ListenPort int     `yaml:"listen_port,omitempty"`
	Ring       RingCfg `yaml:"ring,omitempty"`

	// Default capacity if none provided while creating each "Group".
	MaxSizeMB int64 `yaml:"max_size__mb,omitempty"`

	// Different timeouts
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval,omitempty"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout,omitempty"`
	WriteByteTimeout  time.Duration `yaml:"write_timeout,omitempty"`
}

func (cfg *EmbeddedCacheSingletonConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.IntVar(&cfg.ListenPort, prefix+"embedded-cache.listen_port", 4100, "The port to use for cache communications across the peers when run in distributed fashion")
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "", f)
	f.Int64Var(&cfg.MaxSizeMB, prefix+"embedded-cache.max-size-mb", 100, "Maximum memory size of the cache in MB.")
	f.DurationVar(&cfg.HeartbeatInterval, prefix+"embedded-cache.heartbeat-interval", time.Second, "If the connection is idle, the interval the cache will send heartbeats")
	f.DurationVar(&cfg.HeartbeatTimeout, prefix+"embedded-cache.heartbeat-timeout", time.Second, "Timeout for heartbeat responses")
	f.DurationVar(&cfg.WriteByteTimeout, prefix+"embeddec-cache.write-timeout", time.Second, "Maximum time for the cache to try writing")
}
