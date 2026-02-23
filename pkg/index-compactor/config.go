package indexcompactor

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
)

type Config struct {
	BuilderConfig      logsobj.BuilderBaseConfig `yaml:"builder_config"`
	CompactionInterval time.Duration             `yaml:"compaction_interval"`
	WindowSize         time.Duration             `yaml:"window_size"`
	BatchSize          int                       `yaml:"batch_size"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix("index-compactor.", f)
	f.DurationVar(&cfg.CompactionInterval, "index-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.WindowSize, "index-compactor.window-size", 2*time.Hour, "Time window size for splitting and gathering merged index output.")
	f.IntVar(&cfg.BatchSize, "index-compactor.batch-size", 100, "Number of source index objects to process per scatter batch.")
}
