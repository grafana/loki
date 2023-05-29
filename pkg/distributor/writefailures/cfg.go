package writefailures

import (
	"flag"
	"path"
)

type Cfg struct {
	AddInsightsLabel bool `yaml:"add_insights_label"`
	LogRate          int  `yaml:"logging_rate"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.AddInsightsLabel, path.Join(prefix, "add-insights-label"), false, "Whether a insight=true should be added to all log messages or not.")

	fs.IntVar(&cfg.LogRate, path.Join(prefix, "log-rate"), 1, "How many log messages can be emitted per second. 0 = none.")
}
