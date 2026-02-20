package indexcompactor

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
)

type Config struct {
	BuilderConfig      logsobj.BuilderBaseConfig `yaml:"builder_config"`
	CompactionInterval time.Duration             `yaml:"compaction_interval"`
	MergeWindow        time.Duration             `yaml:"merge_window"`
	BatchSize          int                       `yaml:"batch_size"`
	GatherStride       time.Duration             `yaml:"gather_stride"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix("index-compactor.", f)
	f.DurationVar(&cfg.CompactionInterval, "index-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.MergeWindow, "index-compactor.merge-window", 2*time.Hour, "Time window size for splitting merged index output.")
	f.IntVar(&cfg.BatchSize, "index-compactor.batch-size", 100, "Number of source index objects to process per scatter batch.")
	f.DurationVar(&cfg.GatherStride, "index-compactor.gather-stride", 3*time.Hour, "Time stride for the gather phase to limit memory.")
}
