package tailproxy

import (
	"flag"
)

// Config for a tailproxy
type Config struct {
	DownstreamURL string `yaml:"downstream_url"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.DownstreamURL, "tailproxy.downstream-url", "", "URL of downstream querier.")
}
