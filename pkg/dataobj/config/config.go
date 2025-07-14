package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/querier"
)

type Config struct {
	Consumer  consumer.Config  `yaml:"consumer"`
	Index     index.Config     `yaml:"index"`
	Querier   querier.Config   `yaml:"querier"`
	Metastore metastore.Config `yaml:"metastore"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Consumer.RegisterFlags(f)
	cfg.Index.RegisterFlags(f)
	cfg.Querier.RegisterFlags(f)
	cfg.Metastore.RegisterFlags(f)
	f.StringVar(&cfg.StorageBucketPrefix, "dataobj-storage-bucket-prefix", "dataobj/", "The prefix to use for the storage bucket.")
}

func (cfg *Config) Validate() error {
	if err := cfg.Consumer.Validate(); err != nil {
		return err
	}
	if err := cfg.Querier.Validate(); err != nil {
		return err
	}
	if err := cfg.Metastore.Validate(); err != nil {
		return err
	}
	return nil
}
