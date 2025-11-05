package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type Config struct {
	Consumer  consumer.Config  `yaml:"consumer"`
	Index     index.Config     `yaml:"index"`
	Metastore metastore.Config `yaml:"metastore"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
	Enabled             bool   `yaml:"enabled"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Consumer.RegisterFlags(f)
	cfg.Index.RegisterFlags(f)
	cfg.Metastore.RegisterFlags(f)
	f.StringVar(
		&cfg.StorageBucketPrefix,
		"dataobj-storage-bucket-prefix",
		"dataobj/",
		"The prefix to use for the storage bucket.",
	)
	f.BoolVar(
		&cfg.Enabled,
		"dataobj.enabled",
		false,
		"Enable data objects.",
	)
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		// Do not validate configuration if disabled.
		return nil
	}
	if err := cfg.Consumer.Validate(); err != nil {
		return err
	}
	if err := cfg.Metastore.Validate(); err != nil {
		return err
	}
	return nil
}
