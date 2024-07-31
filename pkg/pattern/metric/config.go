package metric

import "flag"

type AggregationConfig struct {
	Enabled             bool `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester metric aggregation is enabled."`
	LogPushObservations bool `yaml:"log_push_observations,omitempty" doc:"description=Whether to log push observations."`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *AggregationConfig) RegisterFlags(fs *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(fs, "")
}

func (cfg *AggregationConfig) RegisterFlagsWithPrefix(fs *flag.FlagSet, prefix string) {
	fs.BoolVar(&cfg.Enabled, prefix+"metric-aggregation.enabled", false, "Flag to enable or disable metric aggregation.")
	fs.BoolVar(&cfg.LogPushObservations, prefix+"metric-aggregation.log-push-observations", false, "Flag to enable or disable logging of push observations.")
}
