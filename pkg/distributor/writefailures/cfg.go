package writefailures

import (
	"flag"
	"path"

	"github.com/grafana/loki/pkg/util/flagext"
)

type Cfg struct {
	LogRate flagext.ByteSize `yaml:"rate" category:"experimental"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	_ = cfg.LogRate.Set("1KB")
	fs.Var(&cfg.LogRate, path.Join(prefix, "rate"), "Experimental: Log volume allowed (per second). Default: 1KB.")
}
