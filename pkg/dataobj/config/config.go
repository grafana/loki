package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/builder"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/engine/compactor"
)

type Config struct {
	Builder builder.Config `yaml:"builder"`
	// Uploader is shared by every target that uploads data objects.
	Uploader  uploader.Config  `yaml:"uploader"`
	Metastore metastore.Config `yaml:"metastore"`
	// Compaction is the dataobj-compaction-planner target's configuration.
	// Disabled by default; setting Compaction.Enabled = true in addition
	// to the top-level Enabled flag opts the deployment in.
	Compaction compactor.Config `yaml:"compaction"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string `yaml:"storage_bucket_prefix"`
	Enabled             bool   `yaml:"enabled"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Builder.RegisterFlags(f)
	cfg.Uploader.RegisterFlagsWithPrefix("dataobj.uploader.", f)
	cfg.Metastore.RegisterFlags(f)
	cfg.Compaction.RegisterFlags(f)
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
	if err := cfg.Builder.Validate(); err != nil {
		return err
	}
	if err := cfg.Uploader.Validate(); err != nil {
		return err
	}
	if err := cfg.Compaction.Validate(); err != nil {
		return err
	}
	return nil
}
