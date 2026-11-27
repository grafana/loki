package chunkbuilder

import (
	"errors"
	"flag"
	"math"
)

// DefaultConsumerGroup is the default Kafka consumer group of the
// chunk-builders. Each chunk-builder commits only the offset of its own
// partition to it.
const DefaultConsumerGroup = "chunk-builder"

// Config configures the chunk-builder.
type Config struct {
	// Enabled turns on the chunk-builder target. Running the target without it
	// does nothing.
	Enabled bool `yaml:"enabled"`

	// Topic is the Kafka topic that the chunk-builder consumes. It is a
	// separate setting from the Kafka topic of the ingesters, so the
	// chunk-builders can consume a topic that the ingesters never read.
	Topic string `yaml:"topic"`

	// PartitionID overrides the Kafka partition that this instance consumes. A
	// negative value derives the partition from the hostname.
	PartitionID int `yaml:"partition_id"`

	// ConsumerGroup is the Kafka consumer group that the chunk-builder resumes
	// from.
	ConsumerGroup string `yaml:"consumer_group"`
}

// RegisterFlags registers the flags of the chunk-builder.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("chunk-builder.", f)
}

// RegisterFlagsWithPrefix registers the flags of the chunk-builder with
// prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(
		&cfg.Enabled,
		prefix+"enabled",
		false,
		"Experimental: Enable the chunk-builder target. Each chunk-builder consumes one partition of a Kafka topic. It is under development and does not build chunks yet.",
	)
	f.StringVar(
		&cfg.Topic,
		prefix+"topic",
		"",
		"The Kafka topic that the chunk-builder consumes. Required when the chunk-builder is enabled.",
	)
	f.IntVar(
		&cfg.PartitionID,
		prefix+"partition-id",
		-1,
		"The Kafka partition to consume. A negative value derives it from the ordinal suffix of the instance hostname, so chunk-builder-3 consumes partition 3.",
	)
	f.StringVar(
		&cfg.ConsumerGroup,
		prefix+"consumer-group",
		DefaultConsumerGroup,
		"The Kafka consumer group whose committed offset the chunk-builder resumes from.",
	)
}

// Validate returns an error if the config is not valid. Loki calls it only
// when the chunk-builder is enabled, because the topic has no default.
func (cfg *Config) Validate() error {
	if cfg.Topic == "" {
		return errors.New("the topic is required")
	}
	if cfg.ConsumerGroup == "" {
		return errors.New("the consumer group is required")
	}
	if cfg.PartitionID > math.MaxInt32 {
		return errors.New("the partition ID must fit in 32 bits")
	}
	return nil
}
