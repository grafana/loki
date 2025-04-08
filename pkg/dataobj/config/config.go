package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer"
	"github.com/grafana/loki/v3/pkg/dataobj/querier"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

type StoreCacheConfig struct {
	Cache           cache.Config `yaml:"cache"`
	EnableIter      bool         `yaml:"enable_iter"`
	EnableGet       bool         `yaml:"enable_get"`
	EnableReadRange bool         `yaml:"enable_readrange"`
}

func (cfg *StoreCacheConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.EnableIter, prefix+"enable-iter", false, "")
	f.BoolVar(&cfg.EnableGet, prefix+"enable-get", false, "")
	f.BoolVar(&cfg.EnableReadRange, prefix+"enable-readrange", false, "")
	cfg.Cache.RegisterFlagsWithPrefix(prefix+"cache.", "", f)
}

type Config struct {
	Consumer   consumer.Config  `yaml:"consumer"`
	Querier    querier.Config   `yaml:"querier"`
	StoreCache StoreCacheConfig `yaml:"store_cache"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Consumer.RegisterFlagsWithPrefix("dataobj-consumer.", f)
	cfg.Querier.RegisterFlagsWithPrefix("dataobj-querier.", f)
	cfg.StoreCache.RegisterFlagsWithPrefix("dataobj-cache.", f)
	f.StringVar(&cfg.StorageBucketPrefix, "dataobj-storage.bucket-prefix", "dataobj/", "The prefix to use for the storage bucket.")
}

func (cfg *Config) Validate() error {
	if err := cfg.Consumer.Validate(); err != nil {
		return err
	}
	if err := cfg.Querier.Validate(); err != nil {
		return err
	}
	return nil
}
