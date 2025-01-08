package limits

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// Config holds the configuration for the limiter service.
type Config struct {
	ConsumerGroup string        `yaml:"consumer_group"`
	KafkaEnabled  bool          `yaml:"kafka_writes_enabled"`
	KafkaConfig   kafka.Config  `yaml:",inline"`
	WindowSize    time.Duration `yaml:"window_size"`
}

// RegisterFlags registers the configuration flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&cfg.ConsumerGroup, "limiter.consumer-group", "loki-limiter", "The consumer group to use for the limiter.")
	fs.BoolVar(&cfg.KafkaEnabled, "limiter.kafka-writes-enabled", false, "Enable writes to Kafka for stream metadata.")
	fs.DurationVar(&cfg.WindowSize, "limiter.window-size", 1*time.Minute, "The window size to use for the limiter.")
}
