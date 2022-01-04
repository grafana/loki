package limit

import (
	"flag"
)

type Config struct {
	ReadlineRate  float64 `yaml:"readline_rate" json:"readline_rate"`
	ReadlineBurst int     `yaml:"readline_burst" json:"readline_burst"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Float64Var(&cfg.ReadlineRate, prefix+"limit.readline-rate", 10000, "promtail readline Rate.")
	f.IntVar(&cfg.ReadlineBurst, prefix+"limit.readline-burst", 10000, "promtail readline Burst.")
}
