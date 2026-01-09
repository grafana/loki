package passthroughgateway

import (
	"flag"
	"net/url"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
)

type Config struct {
	ListenAddr            string
	MetricsAddr           string
	WriteUpstreamEndpoint string
	ReadUpstreamEndpoint  string
	DefaultTenant         string
	// Server TLS configuration
	TLSCertFile string
	TLSKeyFile  string
	// Client TLS configuration
	TLSClientCAFile string
	TLSClientAuth   string
	// Upstream TLS configuration
	UpstreamCAFile   string
	UpstreamCertFile string
	UpstreamKeyFile  string
	// Generic TLS configuration
	TLSMinVersion   string
	TLSMaxVersion   string
	TLSCipherSuites stringSlice
	TLSCurvePrefs   stringSlice
}

// stringSlice is a custom flag type for comma-separated strings.
type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(value string) error {
	if value == "" {
		return nil
	}
	*s = strings.Split(value, ",")
	return nil
}

// TLSOptions returns the TLS configuration options.
func (c *Config) TLSOptions() *TLSConfig {
	return &TLSConfig{
		MinVersion:   c.TLSMinVersion,
		MaxVersion:   c.TLSMaxVersion,
		CipherSuites: c.TLSCipherSuites,
		CurvePrefs:   c.TLSCurvePrefs,
		ClientAuth:   c.TLSClientAuth,
	}
}

// RegisterFlags registers the configuration flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.ListenAddr, "listen-addr", ":8080", "Address for the server to listen on.")
	f.StringVar(&c.MetricsAddr, "metrics-addr", ":9090", "Address for health and metrics endpoints.")
	f.StringVar(&c.WriteUpstreamEndpoint, "write-upstream-endpoint", "", "Loki write path upstream endpoint (distributor).")
	f.StringVar(&c.ReadUpstreamEndpoint, "read-upstream-endpoint", "", "Loki read path upstream endpoint (query-frontend).")
	f.StringVar(&c.DefaultTenant, "default-tenant", "", "Default tenant ID to use when X-Scope-OrgID header is not set. If empty, requests without X-Scope-OrgID are rejected.")
	// Server TLS configuration flags
	f.StringVar(&c.TLSCertFile, "tls-cert-file", "", "Path to the server TLS certificate file.")
	f.StringVar(&c.TLSKeyFile, "tls-key-file", "", "Path to the server TLS private key file.")
	// Client TLS configuration flags
	f.StringVar(&c.TLSClientCAFile, "tls-client-ca-file", "", "Path to the CA certificate for verifying client certificates.")
	f.StringVar(&c.TLSClientAuth, "tls-client-auth", "NoClientCert", "Client certificate auth mode (NoClientCert, RequestClientCert, RequireAnyClientCert, VerifyClientCertIfGiven, RequireAndVerifyClientCert).")
	// Upstream TLS configuration flags
	f.StringVar(&c.UpstreamCAFile, "upstream-ca-file", "", "Path to the CA certificate for verifying the upstream server.")
	f.StringVar(&c.UpstreamCertFile, "upstream-cert-file", "", "Path to the client certificate for upstream mTLS.")
	f.StringVar(&c.UpstreamKeyFile, "upstream-key-file", "", "Path to the client private key for upstream mTLS.")
	// Generic TLS configuration flags
	f.StringVar(&c.TLSMinVersion, "tls-min-version", "VersionTLS12", "Minimum TLS version (VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13).")
	f.StringVar(&c.TLSMaxVersion, "tls-max-version", "", "Maximum TLS version (VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13). Empty means no maximum.")
	f.Var(&c.TLSCipherSuites, "tls-cipher-suites", "Comma-separated list of TLS cipher suites.")
	f.Var(&c.TLSCurvePrefs, "tls-curve-preferences", "Comma-separated list of curve preferences (X25519, CurveP256, CurveP384, CurveP521).")
}

// Validate checks that all required configuration is provided and valid.
func (c *Config) Validate() error {
	if c.WriteUpstreamEndpoint == "" {
		return kverrors.New("-write-upstream-endpoint is required")
	}

	if _, err := url.Parse(c.WriteUpstreamEndpoint); err != nil {
		return kverrors.New("-write-upstream-endpoint is not a valid URL: " + err.Error())
	}

	if c.ReadUpstreamEndpoint == "" {
		return kverrors.New("-read-upstream-endpoint is required")
	}

	if _, err := url.Parse(c.ReadUpstreamEndpoint); err != nil {
		return kverrors.New("-read-upstream-endpoint is not a valid URL: " + err.Error())
	}

	return nil
}

// TLSEnabled returns true if TLS certificates are configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
