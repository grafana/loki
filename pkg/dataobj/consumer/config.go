package consumer

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

type Config struct {
	dataobj.BuilderConfig
	TenantID string `yaml:"tenant_id"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

func (cfg *Config) Validate() error {
	if cfg.TenantID == "" {
		return errors.New("tenantID is required")
	}
	return cfg.BuilderConfig.Validate()
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.TenantID, prefix+"tenant-id", "fake", "The tenant ID to use for the data object builder.")
	f.StringVar(&cfg.StorageBucketPrefix, prefix+"storage-bucket-prefix", "dataobj/", "The prefix to use for the storage bucket.")
}
