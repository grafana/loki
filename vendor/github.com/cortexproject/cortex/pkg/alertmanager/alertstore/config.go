package alertstore

import (
	"flag"

	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertstore/configdb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alertstore/local"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
)

// LegacyConfig configures the alertmanager storage backend using the legacy storage clients.
// TODO remove this legacy config in Cortex 1.11.
type LegacyConfig struct {
	Type     string        `yaml:"type"`
	ConfigDB client.Config `yaml:"configdb"`

	// Object Storage Configs
	Azure azure.BlobStorageConfig `yaml:"azure"`
	GCS   gcp.GCSConfig           `yaml:"gcs"`
	S3    aws.S3Config            `yaml:"s3"`
	Local local.StoreConfig       `yaml:"local"`
}

// RegisterFlags registers flags.
func (cfg *LegacyConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ConfigDB.RegisterFlagsWithPrefix("alertmanager.", f)
	f.StringVar(&cfg.Type, "alertmanager.storage.type", configdb.Name, "Type of backend to use to store alertmanager configs. Supported values are: \"configdb\", \"gcs\", \"s3\", \"local\".")

	cfg.Azure.RegisterFlagsWithPrefix("alertmanager.storage.", f)
	cfg.GCS.RegisterFlagsWithPrefix("alertmanager.storage.", f)
	cfg.S3.RegisterFlagsWithPrefix("alertmanager.storage.", f)
	cfg.Local.RegisterFlagsWithPrefix("alertmanager.storage.", f)
}

// Validate config and returns error on failure
func (cfg *LegacyConfig) Validate() error {
	if err := cfg.Azure.Validate(); err != nil {
		return errors.Wrap(err, "invalid Azure Storage config")
	}
	if err := cfg.S3.Validate(); err != nil {
		return errors.Wrap(err, "invalid S3 Storage config")
	}
	return nil
}

// IsDefaults returns true if the storage options have not been set.
func (cfg *LegacyConfig) IsDefaults() bool {
	return cfg.Type == configdb.Name && cfg.ConfigDB.ConfigsAPIURL.URL == nil
}

// Config configures a the alertmanager storage backend.
type Config struct {
	bucket.Config `yaml:",inline"`
	ConfigDB      client.Config     `yaml:"configdb"`
	Local         local.StoreConfig `yaml:"local"`
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "alertmanager-storage."

	cfg.ExtraBackends = []string{configdb.Name, local.Name}
	cfg.ConfigDB.RegisterFlagsWithPrefix(prefix, f)
	cfg.Local.RegisterFlagsWithPrefix(prefix, f)
	cfg.RegisterFlagsWithPrefix(prefix, f)
}

// IsFullStateSupported returns if the given configuration supports access to FullState objects.
func (cfg *Config) IsFullStateSupported() bool {
	for _, backend := range bucket.SupportedBackends {
		if cfg.Backend == backend {
			return true
		}
	}
	return false
}
