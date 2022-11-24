package compactor

import (
	"flag"
	"net/http"
	"time"

	"github.com/grafana/dskit/crypto/tls"
)

// Config for compactor's generation-number client
type ClientConfig struct {
	TLSEnabled bool             `yaml:"tls_enabled"`
	TLS        tls.ClientConfig `yaml:",inline"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *ClientConfig) RegisterFlags(f *flag.FlagSet) {
	prefix := "boltdb.shipper.compactor.client"
	f.BoolVar(&cfg.TLSEnabled, prefix+".tls-enabled", false,
		"Enable TLS in the HTTP client. This flag needs to be enabled when any other TLS flag is set. If set to false, insecure connection to HTTP server will be used.")
	cfg.TLS.RegisterFlagsWithPrefix(prefix, f)
}

// NewDeleteHTTPClient return a pointer to a http client instance based on the
// delete client tls settings.
func NewCompactorHTTPClient(cfg ClientConfig) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 250
	transport.MaxIdleConnsPerHost = 250

	if cfg.TLSEnabled {
		tlsCfg, err := cfg.TLS.GetTLSConfig()
		if err != nil {
			return nil, err
		}

		transport.TLSClientConfig = tlsCfg
	}

	return &http.Client{Timeout: 5 * time.Second, Transport: transport}, nil
}
