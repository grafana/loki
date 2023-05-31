package writefailures

import (
	"flag"
	"path"
)

type Cfg struct {
	LogRate int `yaml:"logging_rate" category:"experimental"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.IntVar(&cfg.LogRate, path.Join(prefix, "log-rate"), 1000 /* 1 KB */, "Experimental and subject to change. Log volume allowed (per second). Default: 1KB.")
}
