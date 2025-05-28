package limits

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/kafka"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	DefaultActiveWindow  = 2 * time.Hour
	DefaultRateWindow    = 5 * time.Minute
	DefaultBucketSize    = 1 * time.Minute
	DefaultEvictInterval = 30 * time.Minute
	DefaultNumPartitions = 64
)

// Config represents the configuration for the ingest limits service.
type Config struct {
	// Enabled enables the ingest limits service.
	Enabled bool `yaml:"enabled"`

	// ActiveWindow defines the duration for which streams are considered
	// active. Streams that have not been updated within the ActiveWindow
	// are considered inactive and are not counted towards limits.
	ActiveWindow time.Duration `yaml:"active_window"`

	// RateWindow defines the time window for rate calculation.
	// This should match the window used in Prometheus rate() queries for consistency,
	// when using the `loki_ingest_limits_ingested_bytes_total` metric.
	RateWindow time.Duration `yaml:"rate_window"`

	// BucketSize defines the size of the buckets used to calculate stream
	// rates. Smaller buckets provide more precise rates but require more
	// memory.
	BucketSize time.Duration `yaml:"bucket_size"`

	// EvictionInterval defines the interval at which old streams are evicted.
	EvictionInterval time.Duration `yaml:"eviction_interval"`

	// The number of partitions for the Kafka topic used to read and write stream metadata.
	// It is fixed, not a maximum.
	NumPartitions int `yaml:"num_partitions"`

	// LifecyclerConfig is the config to build a ring lifecycler.
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
	KafkaConfig      kafka.Config          `yaml:"kafka_config,omitempty"`

	// Deprecated.
	WindowSize     time.Duration `yaml:"window_size" doc:"hidden|deprecated"`
	BucketDuration time.Duration `yaml:"bucket_duration" doc:"hidden|deprecated"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)
	cfg.KafkaConfig.RegisterFlagsWithPrefix("ingest-limits.kafka_config.", f)

	f.BoolVar(
		&cfg.Enabled,
		"ingest-limits.enabled",
		false,
		"Enable the ingest limits service.",
	)
	f.DurationVar(
		&cfg.ActiveWindow,
		"ingest-limits.active-window",
		DefaultActiveWindow,
		"The duration for which which streams are considered active. Streams that have not been updated within this window are considered inactive and not counted towards limits.",
	)
	f.DurationVar(
		&cfg.RateWindow,
		"ingest-limits.rate-window",
		DefaultRateWindow,
		"The time window for rate calculation. This should match the window used in Prometheus rate() queries for consistency.",
	)
	f.DurationVar(
		&cfg.BucketSize,
		"ingest-limits.bucket-size",
		DefaultBucketSize,
		"The size of the buckets used to calculate stream rates. Smaller buckets provide more precise rates but require more memory.",
	)
	f.DurationVar(
		&cfg.EvictionInterval,
		"ingest-limits.eviction-interval",
		DefaultEvictInterval,
		"The interval at which old streams are evicted.",
	)
	f.IntVar(
		&cfg.NumPartitions,
		"ingest-limits.num-partitions",
		DefaultNumPartitions,
		"The number of partitions for the Kafka topic used to read and write stream metadata. It is fixed, not a maximum.",
	)
}

func (cfg *Config) Validate() error {
	if cfg.ActiveWindow <= 0 {
		return errors.New("active-window must be greater than 0")
	}
	if cfg.RateWindow <= 0 {
		return errors.New("rate-window must be greater than 0")
	}
	if cfg.BucketSize <= 0 {
		return errors.New("bucket-size must be greater than 0")
	}
	if cfg.RateWindow < cfg.BucketSize {
		return errors.New("rate-window must be greater than or equal to bucket-size")
	}
	if cfg.EvictionInterval <= 0 {
		return errors.New("eviction-interval must be greater than 0")
	}
	if cfg.NumPartitions <= 0 {
		return errors.New("num-partitions must be greater than 0")
	}
	return nil
}
