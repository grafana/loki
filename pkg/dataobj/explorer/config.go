package explorer

import "flag"

// Config holds the configuration for the explorer service
type Config struct {
	// StorageBucketPrefix is the prefix to use when exploring the bucket
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

// RegisterFlags registers the flags for the explorer configuration
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.StorageBucketPrefix, "dataobj-explorer.storage-bucket-prefix", "dataobj/", "Prefix to use when exploring the bucket. If set, only objects under this prefix will be visible.")
}
