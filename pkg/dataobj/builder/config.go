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

	// FlushConcurrency is the maximum number of data objects that one flush
	// builds, uploads and indexes at the same time. It must be at least 1.
	FlushConcurrency int `yaml:"flush_concurrency"`

	// SortConcurrency is the maximum number of goroutines that sort the logs
	// of one tenant in one data object. It must be at least 1.
	SortConcurrency int `yaml:"sort_concurrency"`

	// This is temporary until we move to kafkav2.
	Topic string `yaml:"topic"`

	// PartitionID overrides the Kafka partition this instance consumes. A negative value (the
	// default) derives the partition from the hostname.
	PartitionID int `yaml:"partition_id"`

	// RequiredReplicasRefreshInterval is the time between two refreshes of
	// loki_dataobj_builder_required_replicas. Only the builder that derives
	// partition 0 from its hostname uses it.
	RequiredReplicasRefreshInterval time.Duration `yaml:"required_replicas_refresh_interval"`
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
	if cfg.FlushConcurrency < 1 {
		return errors.New("flush concurrency must be at least 1")
	}
	if cfg.SortConcurrency < 1 {
		return errors.New("sort concurrency must be at least 1")
	}
	if cfg.RequiredReplicasRefreshInterval <= 0 {
		return errors.New("required replicas refresh interval must be positive")
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
	f.IntVar(
		&cfg.FlushConcurrency,
		prefix+"flush-concurrency",
		1,
		"The maximum number of data objects that one flush builds, uploads and indexes at the same time. Each extra object in progress adds the memory to sort, upload and index it. If one object fails, the flush cancels the others. The builder commits the Kafka offset only after every object succeeds.",
	)
	f.IntVar(
		&cfg.SortConcurrency,
		prefix+"sort-concurrency",
		1,
		"The maximum number of goroutines that sort the logs of one tenant in one data object. Each goroutine sorts a range of streams of about the target section size, and adds the memory of one section. A flush can sort up to flush-concurrency objects at the same time, each with this many goroutines.",
	)
	f.IntVar(
		&cfg.PartitionID,
		prefix+"partition-id",
		-1,
		"The Kafka partition to consume. A negative value derives it from the instance hostname instead.",
	)
	f.DurationVar(
		&cfg.RequiredReplicasRefreshInterval,
		prefix+"required-replicas-refresh-interval",
		30*time.Second,
		"How often the builder that consumes partition 0 reads the offsets of every partition to refresh the loki_dataobj_builder_required_replicas metric.",
	)
}
