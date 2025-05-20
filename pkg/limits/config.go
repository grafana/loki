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
	DefaultActiveWindow   = 1 * time.Hour
	DefaultRateWindow     = 5 * time.Minute
	DefaultBucketDuration = 1 * time.Minute
	DefaultNumPartitions  = 64
)

// Config represents the configuration for the ingest limits service.
type Config struct {
	// Enabled enables the ingest limits service.
	Enabled bool `yaml:"enabled"`

	// ActiveWindow contains the duration for which streams are considered
	// active. Streams that have not been updated within the ActiveWindow
	// are considered inactive and are not counted towards limits.
	ActiveWindow time.Duration `yaml:"active_window"`

	// RateWindow defines the time window for rate calculation.
	// This should match the window used in Prometheus rate() queries for consistency,
	// when using the `loki_ingest_limits_ingested_bytes_total` metric.
	// Defaults to 5 minutes if not specified.
	RateWindow time.Duration `yaml:"rate_window"`

	// BucketDuration defines the granularity of time buckets used for sliding window rate calculation.
	// Smaller buckets provide more precise rate tracking but require more memory.
	// Defaults to 1 minute if not specified.
	BucketDuration time.Duration `yaml:"bucket_duration"`

	// LifecyclerConfig is the config to build a ring lifecycler.
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	KafkaConfig kafka.Config `yaml:"-"`

	// The number of partitions for the Kafka topic used to read and write stream metadata.
	// It is fixed, not a maximum.
	NumPartitions int `yaml:"num_partitions"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)
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
		&cfg.BucketDuration,
		"ingest-limits.bucket-duration",
		DefaultBucketDuration,
		"The granularity of time buckets used for sliding window rate calculation. Smaller buckets provide more precise rate tracking but require more memory.",
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
	if cfg.BucketDuration <= 0 {
		return errors.New("bucket-duration must be greater than 0")
	}
	if cfg.RateWindow < cfg.BucketDuration {
		return errors.New("rate-window must be greater than or equal to bucket-duration")
	}
	if cfg.NumPartitions <= 0 {
		return errors.New("num-partitions must be greater than 0")
	}
	return nil
}
