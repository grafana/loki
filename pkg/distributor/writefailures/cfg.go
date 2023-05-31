package writefailures

import (
	"flag"

	"github.com/grafana/loki/pkg/util/flagext"
)

type Cfg struct {
	LogRate flagext.ByteSize `yaml:"rate" category:"experimental"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	_ = cfg.LogRate.Set("1KB")
	fs.Var(&cfg.LogRate, prefix+".rate", "Experimental and subject to change. Log volume allowed (per second). Default: 1KB.")
}
