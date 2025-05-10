package limits

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/kafka"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// Config represents the configuration for the ingest limits service.
type Config struct {
	// Enabled enables the ingest limits service.
	Enabled bool `yaml:"enabled"`

	// WindowSize defines the time window for which stream metadata is considered active.
	// Stream metadata older than WindowSize will be evicted from the metadata map.
	WindowSize time.Duration `yaml:"window_size"`

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

	// TODO(grobinson): These will be removed when we figure out how to support
	// different addresses for read and write clients in kafka.Config,
	KafkaReadAddress  string `yaml:"kafka_read_address"`
	KafkaWriteAddress string `yaml:"kafka_write_address"`

	// The number of partitions for the Kafka topic used to read and write stream metadata.
	// It is fixed, not a maximum.
	NumPartitions int `yaml:"num_partitions"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)
	f.BoolVar(&cfg.Enabled, "ingest-limits.enabled", false, "Enable the ingest limits service.")
	f.DurationVar(&cfg.WindowSize, "ingest-limits.window-size", 1*time.Hour, "The time window for which stream metadata is considered active.")
	f.DurationVar(&cfg.RateWindow, "ingest-limits.rate-window", 5*time.Minute, "The time window for rate calculation. This should match the window used in Prometheus rate() queries for consistency.")
	f.DurationVar(&cfg.BucketDuration, "ingest-limits.bucket-duration", 1*time.Minute, "The granularity of time buckets used for sliding window rate calculation. Smaller buckets provide more precise rate tracking but require more memory.")
	f.IntVar(&cfg.NumPartitions, "ingest-limits.num-partitions", 64, "The number of partitions for the Kafka topic used to read and write stream metadata. It is fixed, not a maximum.")
	f.StringVar(&cfg.KafkaReadAddress, "ingest-limits.kafka-read-address", "", "The address of the seed broker for the Kafka read client.")
	f.StringVar(&cfg.KafkaWriteAddress, "ingest-limits.kafka-write-address", "", "The address of the seed broker for the Kafka write client.")
}

func (cfg *Config) Validate() error {
	if cfg.WindowSize <= 0 {
		return errors.New("window-size must be greater than 0")
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
