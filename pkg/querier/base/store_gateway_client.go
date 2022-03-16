package base

import (
	"flag"

	"github.com/grafana/dskit/crypto/tls"
)

type ClientConfig struct {
	TLSEnabled bool             `yaml:"tls_enabled"`
	TLS        tls.ClientConfig `yaml:",inline"`
}

func (cfg *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.TLSEnabled, prefix+".tls-enabled", cfg.TLSEnabled, "Enable TLS for gRPC client connecting to store-gateway.")
	cfg.TLS.RegisterFlagsWithPrefix(prefix, f)
}
