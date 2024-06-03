package metric

import "flag"

type AggregationConfig struct {
	Enabled bool `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester metric aggregation is enabled."`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *AggregationConfig) RegisterFlags(fs *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(fs, "")
}

func (cfg *AggregationConfig) RegisterFlagsWithPrefix(fs *flag.FlagSet, prefix string) {
	fs.BoolVar(&cfg.Enabled, prefix+"metric-aggregation.enabled", false, "Flag to enable or disable metric aggregation.")
}
