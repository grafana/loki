package limit

import (
	"flag"
)

type Config struct {
	ReadlineRate        float64 `mapstructure:"readline_rate" yaml:"readline_rate" json:"readline_rate"`
	ReadlineBurst       int     `mapstructure:"readline_burst" yaml:"readline_burst" json:"readline_burst"`
	ReadlineRateEnabled bool    `mapstructure:"readline_rate_enabled,omitempty" yaml:"readline_rate_enabled,omitempty"  json:"readline_rate_enabled"`
	ReadlineRateDrop    bool    `mapstructure:"readline_rate_drop,omitempty" yaml:"readline_rate_drop,omitempty"  json:"readline_rate_drop"`
	MaxStreams          int     `mapstructure:"max_streams" yaml:"max_streams" json:"max_streams"`
	MaxLineSize         int     `mapstructure:"max_line_size" yaml:"max_line_size" json:"max_line_size"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Float64Var(&cfg.ReadlineRate, prefix+"limit.readline-rate", 10000, "The rate limit in log lines per second that this instance of Promtail may push to Loki.")
	f.IntVar(&cfg.ReadlineBurst, prefix+"limit.readline-burst", 10000, "The cap in the quantity of burst lines that this instance of Promtail may push to Loki.")
	f.BoolVar(&cfg.ReadlineRateEnabled, prefix+"limit.readline-rate-enabled", false, "When true, enforces rate limiting on this instance of Promtail.")
	f.BoolVar(&cfg.ReadlineRateDrop, prefix+"limit.readline-rate-drop", true, "When true, exceeding the rate limit causes this instance of Promtail to discard log lines, rather than sending them to Loki.")
	f.IntVar(&cfg.MaxStreams, prefix+"max-streams", 0, "Maximum number of active streams. 0 to disable.")
	f.IntVar(&cfg.MaxLineSize, prefix+"max-line-size", 0, "Maximum log line byte size allowed without dropping. 0 to disable.")
}
