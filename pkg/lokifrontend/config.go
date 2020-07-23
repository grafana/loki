package lokifrontend

import (
	"flag"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
)

type Config struct {
	frontend.Config `yaml:",inline"`
	TailProxyUrl    string `yaml:"tail_proxy_url"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	f.StringVar(&cfg.TailProxyUrl, "frontend.tail-proxy-url", "", "URL of querier for tail proxy.")
}