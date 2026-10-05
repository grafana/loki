package builder

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
)

type Config struct {
	// LogsobjBuilder controls the construction of the data objects built from
	// the consumed records.
	LogsobjBuilder logsobj.BuilderBaseConfig `yaml:"logsobj_builder"`

	// IndexobjBuilder controls the construction of the index object built for
	// each data object.
	IndexobjBuilder logsobj.BuilderBaseConfig `yaml:"indexobj_builder"`

	IdleFlushTimeout time.Duration `yaml:"idle_flush_timeout"`
	MaxBuilderAge    time.Duration `yaml:"max_builder_age"`

	// This is temporary until we move to kafkav2.
	Topic string `yaml:"topic"`
}

func (cfg *Config) Validate() error {
	if err := cfg.LogsobjBuilder.Validate(); err != nil {
		return err
	}
	if err := cfg.IndexobjBuilder.Validate(); err != nil {
		return err
	}
	if cfg.Topic == "" {
		return errors.New("topic is required")
	}
	return nil
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj.builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	// These configs do not have defaults in the flagset so default values must be Set before registering
	// the flags to be documented correctly.
	_ = cfg.LogsobjBuilder.TargetPageSize.Set("1MB")
	_ = cfg.LogsobjBuilder.TargetObjectSize.Set("512MB") // compressed
	_ = cfg.LogsobjBuilder.BufferSize.Set("128MB")
	_ = cfg.LogsobjBuilder.TargetSectionSize.Set("512MB") // uncompressed
	cfg.LogsobjBuilder.RegisterFlagsWithPrefix(prefix+"logsobj-builder.", f)

	_ = cfg.IndexobjBuilder.TargetPageSize.Set("128KB")   // smaller pages gives more opportunities to prune
	_ = cfg.IndexobjBuilder.TargetObjectSize.Set("512MB") // compressed
	_ = cfg.IndexobjBuilder.BufferSize.Set("128MB")
	_ = cfg.IndexobjBuilder.TargetSectionSize.Set("512MB") // uncompressed
	cfg.IndexobjBuilder.RegisterFlagsWithPrefix(prefix+"indexobj-builder.", f)
	// BuilderBaseConfig registers the estimated compression ratio with a fixed
	// default, so override it for index objects after registration.
	cfg.IndexobjBuilder.EstimatedCompressionRatio = 1
	f.Lookup(prefix + "indexobj-builder.estimated-compression-ratio").DefValue = "1"

	f.StringVar(
		&cfg.Topic,
		prefix+"topic",
		"",
		"The name of the Kafka topic.",
	)
	f.DurationVar(
		&cfg.IdleFlushTimeout,
		prefix+"idle-flush-timeout",
		time.Hour,
		"The maximum amount of time to wait in seconds before flushing an object that is no longer receiving new writes.",
	)
	f.DurationVar(&cfg.MaxBuilderAge,
		prefix+"max-builder-age",
		time.Hour,
		"The maximum amount of time to accumulate data in a builder before flushing it. Defaults to 1 hour.",
	)
}
