package metastore

import (
	"flag"

	"github.com/grafana/dskit/flagext"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	Storage StorageConfig `yaml:"storage" experimental:"true"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "dataobj-metastore."
	c.Storage.RegisterFlagsWithPrefix(prefix, f)
}

// Validate validates the metastore settings.
func (c *Config) Validate() error {
	return nil
}

type StorageConfig struct {
	IndexStoragePrefix string                 `yaml:"index_storage_prefix" experimental:"true"`
	EnabledTenantIDs   flagext.StringSliceCSV `yaml:"enabled_tenant_ids" experimental:"true"`
}

func (c *StorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.IndexStoragePrefix, prefix+"index-storage-prefix", "index/v0/", "Experimental: A prefix to use for storing indexes in object storage. Used to separate the metastore & index files during initial testing.")
	f.Var(&c.EnabledTenantIDs, prefix+"enabled-tenant-ids", "Experimental: A list of tenant IDs to enable index building for. If empty, all tenants will be enabled.")
}
