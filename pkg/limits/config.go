package limits

import (
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
	// LifecyclerConfig is the config to build a ring lifecycler.
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	KafkaConfig kafka.Config `yaml:"-"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)

	f.BoolVar(&cfg.Enabled, "ingest-limits.enabled", false, "Enable the ingest limits service.")
	f.DurationVar(&cfg.WindowSize, "ingest-limits.window-size", 1*time.Hour, "The time window for which stream metadata is considered active.")
}
