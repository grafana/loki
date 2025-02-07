package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer"
	"github.com/grafana/loki/v3/pkg/dataobj/querier"
)

type Config struct {
	Consumer consumer.Config `yaml:"consumer"`
	Querier  querier.Config  `yaml:"querier"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Consumer.RegisterFlags(f)
	cfg.Querier.RegisterFlags(f)
	f.StringVar(&cfg.StorageBucketPrefix, "dataobj-storage-bucket-prefix", "dataobj/", "The prefix to use for the storage bucket.")
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
