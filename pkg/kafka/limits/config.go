package limits

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// Config holds configuration for the IngestLimiter service.
type Config struct {
	KafkaEnabled bool          `yaml:"kafka_enabled"`
	KafkaConfig  kafka.Config  `yaml:",inline"`
	WindowSize   time.Duration `yaml:"window_size"`
}

// RegisterFlags registers the configuration flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cfg.KafkaEnabled, "ingest-limiter.kafka-enabled", false, "Enable writes to Kafka for stream metadata.")
	fs.DurationVar(&cfg.WindowSize, "ingest-limiter.window-size", 1*time.Minute, "The window size to use for the limiter.")
}
