package index

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
)

type Config struct {
	indexobj.BuilderConfig `yaml:",inline"`
	EventsPerIndex         int           `yaml:"events_per_index" experimental:"true"`
	FlushInterval          time.Duration `yaml:"flush_interval" experimental:"true"`
	MaxIdleTime            time.Duration `yaml:"max_idle_time" experimental:"true"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.IntVar(&cfg.EventsPerIndex, prefix+"events-per-index", 32, "Experimental: The number of events to batch before building an index")
	f.DurationVar(&cfg.FlushInterval, prefix+"flush-interval", 1*time.Minute, "Experimental: How often to check for stale partitions to flush")
	f.DurationVar(&cfg.MaxIdleTime, prefix+"max-idle-time", 30*time.Minute, "Experimental: Maximum time to wait before flushing buffered events")
}
