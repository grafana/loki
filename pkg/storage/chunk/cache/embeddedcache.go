package cache

import (
	"flag"
	"time"
)

const (
	DefaultPurgeInterval = 1 * time.Minute
)

// EmbeddedCacheConfig represents in-process embedded cache config.
type EmbeddedCacheConfig struct {
	Enabled   bool          `yaml:"enabled,omitempty"`
	MaxSizeMB int64         `yaml:"max_size_mb"`
	TTL       time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `DefaultPurgeInterval`
	PurgeInterval time.Duration `yaml:"-"`
}

func (cfg *EmbeddedCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"embedded-cache.enabled", false, description+"Whether embedded cache is enabled.")
	f.Int64Var(&cfg.MaxSizeMB, prefix+"embedded-cache.max-size-mb", 100, description+"Maximum memory size of the cache in MB.")
	f.DurationVar(&cfg.TTL, prefix+"embedded-cache.ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")

}

func (cfg *EmbeddedCacheConfig) IsEnabled() bool {
	return cfg.Enabled
}
