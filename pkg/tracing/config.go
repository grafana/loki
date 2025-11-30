package tracing

import (
	"flag"
)

type Config struct {
	Enabled        bool `yaml:"enabled"`
	FilterGCSSpans bool `yaml:"filter_gcs_spans"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("tracing.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", true, "Set to false to disable tracing.")
	f.BoolVar(&cfg.FilterGCSSpans, prefix+"filter-gcs-spans", true, "Set to true to drops all spans from the GCS client library. This prevents the GCS client from creating millions of spans in high-throughput production environments.")
}
