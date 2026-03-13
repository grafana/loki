package passthroughgateway

import (
	"flag"
	"net/url"
	"strings"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
)

type LokiConfig struct {
	DistributorEndpoint   string
	QueryFrontendEndpoint string
	Timeout               time.Duration
	CAFile                string
	CertFile              string
	KeyFile               string
}

type Config struct {
	ListenAddr    string
	AdminAddr     string
	DefaultTenant string
	Loki          LokiConfig
	// Server TLS configuration
	TLSCertFile string
	TLSKeyFile  string
	// Client TLS configuration
	TLSClientCAFile string
	TLSClientAuth   string
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
	f.StringVar(&c.AdminAddr, "admin-addr", ":9090", "Address for admin endpoints (metrics, health, readiness).")
	f.StringVar(&c.DefaultTenant, "default-tenant", "", "Default tenant ID to use when X-Scope-OrgID header is not set. If empty, requests without X-Scope-OrgID are rejected.")
	// Loki upstream configuration flags
	f.StringVar(&c.Loki.DistributorEndpoint, "loki-distributor-endpoint", "", "Upstream URL of the Loki distributor (write path).")
	f.StringVar(&c.Loki.QueryFrontendEndpoint, "loki-query-frontend-endpoint", "", "Upstream URL of the Loki query frontend (read path).")
	f.DurationVar(&c.Loki.Timeout, "loki-timeout", 60*time.Second, "Timeout for upstream Loki requests. Set to 0 for no timeout.")
	f.StringVar(&c.Loki.CAFile, "loki-ca-file", "", "Path to the CA certificate for verifying the Loki server.")
	f.StringVar(&c.Loki.CertFile, "loki-cert-file", "", "Path to the client certificate for Loki mTLS.")
	f.StringVar(&c.Loki.KeyFile, "loki-key-file", "", "Path to the client private key for Loki mTLS.")
	// Server TLS configuration flags
	f.StringVar(&c.TLSCertFile, "tls-cert-file", "", "Path to the server TLS certificate file.")
	f.StringVar(&c.TLSKeyFile, "tls-key-file", "", "Path to the server TLS private key file.")
	// Client TLS configuration flags
	f.StringVar(&c.TLSClientCAFile, "tls-client-ca-file", "", "Path to the CA certificate for verifying client certificates.")
	f.StringVar(&c.TLSClientAuth, "tls-client-auth", "NoClientCert", "Client certificate auth mode (NoClientCert, RequestClientCert, RequireAnyClientCert, VerifyClientCertIfGiven, RequireAndVerifyClientCert).")
	// Generic TLS configuration flags
	f.StringVar(&c.TLSMinVersion, "tls-min-version", "VersionTLS12", "Minimum TLS version (VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13).")
	f.StringVar(&c.TLSMaxVersion, "tls-max-version", "", "Maximum TLS version (VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13). Empty means no maximum.")
	f.Var(&c.TLSCipherSuites, "tls-cipher-suites", "Comma-separated list of TLS cipher suites.")
	f.Var(&c.TLSCurvePrefs, "tls-curve-preferences", "Comma-separated list of curve preferences (X25519, CurveP256, CurveP384, CurveP521).")
}

// Validate checks that all required configuration is provided and valid.
func (c *Config) Validate() error {
	if c.Loki.DistributorEndpoint == "" {
		return kverrors.New("-loki-distributor-endpoint is required")
	}

	if _, err := url.Parse(c.Loki.DistributorEndpoint); err != nil {
		return kverrors.New("-loki-distributor-endpoint is not a valid URL: " + err.Error())
	}

	if c.Loki.QueryFrontendEndpoint == "" {
		return kverrors.New("-loki-query-frontend-endpoint is required")
	}

	if _, err := url.Parse(c.Loki.QueryFrontendEndpoint); err != nil {
		return kverrors.New("-loki-query-frontend-endpoint is not a valid URL: " + err.Error())
	}

	return nil
}

// TLSEnabled returns true if TLS certificates are configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
