package metastore

import (
	"flag"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	IndexStoragePrefix string `yaml:"index_storage_prefix" experimental:"true"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "dataobj-metastore."
	f.StringVar(&c.IndexStoragePrefix, prefix+"index-storage-prefix", "index/v0", "Experimental: A prefix to use for storing indexes in object storage. Used for testing only.")
}
