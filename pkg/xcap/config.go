package xcap

import (
	"flag"
)

// Config holds the configuration options for xcap tracing.
type Config struct {
	// SeparateDetailedTrace controls whether xcap exports traces as two separate
	// traces (a summary span in the global trace linked to a detailed trace) or
	// as a single trace with all spans nested under the parent context.
	//
	// When enabled (default), xcap creates:
	//   1. A summary span in the global trace with aggregated statistics and a
	//      span link to the detailed trace.
	//   2. A separate detailed trace where xcap is the root span, containing all
	//      individual region spans with their full hierarchy.
	//
	// When disabled, xcap creates all spans directly under the parent context,
	// which can result in deeply nested traces that are harder to navigate.
	SeparateDetailedTrace bool `yaml:"separate_detailed_trace" category:"experimental"`
}

// RegisterFlags registers flags for the xcap configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("xcap.", f)
}

// RegisterFlagsWithPrefix registers flags for the xcap configuration with a prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.SeparateDetailedTrace, prefix+"separate-detailed-trace", true,
		"When enabled, xcap exports traces as a summary span (in the global trace) linked to a separate detailed trace.")
}
