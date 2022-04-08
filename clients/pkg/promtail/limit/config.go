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
	f.Float64Var(&cfg.ReadlineRate, prefix+"limit.readline-rate", 10000, "The rate limit in log lines per second that this instance of Promtail may push to Loki.")
	f.IntVar(&cfg.ReadlineBurst, prefix+"limit.readline-burst", 10000, "The cap in the quantity of burst lines that this instance of Promtail may push to Loki.")
	f.BoolVar(&cfg.ReadlineRateEnabled, prefix+"limit.readline-rate-enabled", false, "When true, enforces rate limiting on this instance of Promtail.")
	f.BoolVar(&cfg.ReadlineRateDrop, prefix+"limit.readline-rate-drop", true, "When true, exceeding the rate limit causes this instance of Promtail to discard log lines, rather than sending them to Loki.")
}
