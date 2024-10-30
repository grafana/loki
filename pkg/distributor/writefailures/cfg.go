package writefailures

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type Cfg struct {
	LogRate flagext.ByteSize `yaml:"rate"`

	AddInsightsLabel bool `yaml:"add_insights_label"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	_ = cfg.LogRate.Set("1KB")
	fs.Var(&cfg.LogRate, prefix+".rate", "Log volume allowed (per second). Default: 1KB.")

	fs.BoolVar(&cfg.AddInsightsLabel, prefix+".add-insights-label", false, "Whether a insight=true key should be logged or not. Default: false.")
}
