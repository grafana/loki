package streamcount

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// Config is the configuration for the [Service].
type Config struct {
	ConsumerGroup string       `yaml:"consumer_group"`
	Topic         string       `yaml:"topic"`
	KafkaConfig   kafka.Config `yaml:"-"`
}

// RegisterFlags registers the flags with the prefix.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.ConsumerGroup, "stream-count.consumer-group", "", "The consumer group.")
	f.StringVar(&cfg.Topic, "stream-count.topic", "", "The topic.")
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if cfg.ConsumerGroup == "" {
		return errors.New("consumer group must be set")
	}
	if cfg.Topic == "" {
		return errors.New("topic must be set")
	}
	return nil
}
