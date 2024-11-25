package http

import (
	"flag"
	"net/http"
	"time"
)

// Config stores the http.Client configuration for the storage clients.
type Config struct {
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool          `yaml:"insecure_skip_verify"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout"`
	MaxIdleConns          int           `yaml:"max_idle_connections"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_connections_per_host"`
	MaxConnsPerHost       int           `yaml:"max_connections_per_host"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`

	TLSConfig TLSConfig `yaml:",inline"`
}

// TLSConfig configures the options for TLS connections.
type TLSConfig struct {
	CAPath     string `yaml:"tls_ca_path" category:"advanced"`
	CertPath   string `yaml:"tls_cert_path" category:"advanced"`
	KeyPath    string `yaml:"tls_key_path" category:"advanced"`
	ServerName string `yaml:"tls_server_name" category:"advanced"`
}

// RegisterFlags registers the flags for the storage HTTP client.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for the storage HTTP client with the provided prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.IdleConnTimeout, prefix+"http.idle-conn-timeout", 90*time.Second, "The time an idle connection will remain idle before closing.")
	f.DurationVar(&cfg.ResponseHeaderTimeout, prefix+"http.response-header-timeout", 2*time.Minute, "The amount of time the client will wait for a servers response headers.")
	f.BoolVar(&cfg.InsecureSkipVerify, prefix+"http.insecure-skip-verify", false, "If the client connects via HTTPS and this option is enabled, the client will accept any certificate and hostname.")
	f.DurationVar(&cfg.TLSHandshakeTimeout, prefix+"tls-handshake-timeout", 10*time.Second, "Maximum time to wait for a TLS handshake. 0 means no limit.")
	f.DurationVar(&cfg.ExpectContinueTimeout, prefix+"expect-continue-timeout", 1*time.Second, "The time to wait for a server's first response headers after fully writing the request headers if the request has an Expect header. 0 to send the request body immediately.")
	f.IntVar(&cfg.MaxIdleConns, prefix+"max-idle-connections", 100, "Maximum number of idle (keep-alive) connections across all hosts. 0 means no limit.")
	f.IntVar(&cfg.MaxIdleConnsPerHost, prefix+"max-idle-connections-per-host", 100, "Maximum number of idle (keep-alive) connections to keep per-host. If 0, a built-in default value is used.")
	f.IntVar(&cfg.MaxConnsPerHost, prefix+"max-connections-per-host", 0, "Maximum number of connections per host. 0 means no limit.")
	cfg.TLSConfig.RegisterFlagsWithPrefix(prefix, f)
}

// RegisterFlagsWithPrefix registers the flags for s3 storage with the provided prefix.
func (cfg *TLSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.CAPath, prefix+"http.tls-ca-path", "", "Path to the CA certificates to validate server certificate against. If not set, the host's root CA certificates are used.")
	f.StringVar(&cfg.CertPath, prefix+"http.tls-cert-path", "", "Path to the client certificate, which will be used for authenticating with the server. Also requires the key path to be configured.")
	f.StringVar(&cfg.KeyPath, prefix+"http.tls-key-path", "", "Path to the key for the client certificate. Also requires the client certificate to be configured.")
	f.StringVar(&cfg.ServerName, prefix+"http.tls-server-name", "", "Override the expected name on the server certificate.")
}
