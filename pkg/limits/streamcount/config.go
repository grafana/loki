package streamcount

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// Config is the configuration for the [Service].
type Config struct {
	// Topic is the topic containing stream metadata records.
	Topic       string       `yaml:"topic"`
	KafkaConfig kafka.Config `yaml:"-"`
}

// RegisterFlags registers the flags with the prefix.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Topic, "stream-count.topic", "", "The topic containing stream metadata records.")
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if cfg.Topic == "" {
		return errors.New("topic must be set")
	}
	return nil
}
