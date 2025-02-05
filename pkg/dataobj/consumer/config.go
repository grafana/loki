package consumer

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
)

type Config struct {
	dataobj.BuilderConfig
	UploaderConfig uploader.Config `yaml:"uploader"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
}

func (cfg *Config) Validate() error {
	if err := cfg.UploaderConfig.Validate(); err != nil {
		return err
	}

	return cfg.BuilderConfig.Validate()
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.UploaderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.StorageBucketPrefix, prefix+"storage-bucket-prefix", "dataobj/", "The prefix to use for the storage bucket.")
}
