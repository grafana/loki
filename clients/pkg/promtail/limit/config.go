package limit

import (
	"flag"
)

type Config struct {
	ReadlineRate        float64 `yaml:"readline_rate" json:"readline_rate"`
	ReadlineBurst       int     `yaml:"readline_burst" json:"readline_burst"`
	ReadlineRateEnabled bool    `yaml:"readline_rate_enabled,omitempty"  json:"readline_rate_enabled"`
	ReadlineRateDrop    bool    `yaml:"readline_rate_drop,omitempty"  json:"readline_rate_drop"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Float64Var(&cfg.ReadlineRate, prefix+"limit.readline-rate", 10000, "promtail readline Rate.")
	f.IntVar(&cfg.ReadlineBurst, prefix+"limit.readline-burst", 10000, "promtail readline Burst.")
	f.BoolVar(&cfg.ReadlineRateEnabled, prefix+"limit.readline-rate-enabled", true, "Set to false to disable readline rate limit.")
	f.BoolVar(&cfg.ReadlineRateDrop, prefix+"limit.readline-rate-drop", true, "Set to true to drop log when rate limit.")
}
