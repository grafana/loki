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
	OutputPrefix       string                    `yaml:"output_prefix"`
	SourcePrefix       string                    `yaml:"source_prefix"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix("index-compactor.", f)
	f.DurationVar(&cfg.CompactionInterval, "index-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.WindowSize, "index-compactor.window-size", 2*time.Hour, "Time window size for splitting and gathering merged index output.")
	f.IntVar(&cfg.BatchSize, "index-compactor.batch-size", 100, "Number of source index objects to process per scatter batch.")
	f.StringVar(&cfg.OutputPrefix, "index-compactor.output-prefix", "index/v0-compacted-test", "Prefix to write the compacted index objects to.")
	f.StringVar(&cfg.SourcePrefix, "index-compactor.source-prefix", "index/v0", "Prefix to read the source index objects from.")
}
