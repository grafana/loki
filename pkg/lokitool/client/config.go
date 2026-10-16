package client

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/config"
)

const (
	defaultAddress    = "http://localhost:3100"
	defaultAuthHeader = "Authorization"
)

// Config holds the settings to connect to a Loki cluster.
//
// The settings that have aliases or several environment variables are
// filled in by Resolve. Use Register to bind the settings to command line flags.
type Config struct {
	Address string
	OrgID   string

	// Username and Password are used for HTTP basic authentication.
	// If only the password is set, the OrgID is used as username.
	Username string
	Password string

	// BearerToken and BearerTokenFile are used for bearer token authentication.
	// The file is read for every request so that rotated tokens are picked up.
	BearerToken     string
	BearerTokenFile string

	// AuthHeader is the name of the header that carries the credentials.
	AuthHeader string

	TLS config.TLSConfig

	// QueryTags is sent in the X-Query-Tags header.
	QueryTags string
	NoCache   bool

	// Retries is the number of times a failed request is retried.
	// MinBackoff and MaxBackoff are the bounds of the wait between retries in seconds.
	Retries    int
	MinBackoff int
	MaxBackoff int

	// ProxyURL is the proxy for all requests. It takes precedence over EnvProxy.
	ProxyURL string
	// EnvProxy enables the proxy settings of the environment (HTTP_PROXY, HTTPS_PROXY, NO_PROXY).
	EnvProxy bool

	// Compress enables compression of responses.
	Compress bool

	// Headers are additional request headers in the format 'Header-Name: value'.
	Headers []string
}

// stringSetting is a string setting that can be set through several flags and
// environment variables.
//
// All flags write to the same variable. Kingpin applies defaults and
// environment variables of flags that are not set in arbitrary order,
// so it is not used here. Resolve applies them in a defined order instead.
type stringSetting struct {
	dst  *string
	help string
	def  string
	// envs are the environment variables in order of precedence.
	envs []string
	// names are the flag names. The first is the canonical name, the others are hidden aliases.
	names []string
}

func (s stringSetting) register(app *kingpin.Application) {
	help := s.help
	if len(s.envs) > 0 {
		help += " Can also be set using " + strings.Join(s.envs, " or ") + " env var."
	}
	if s.def != "" {
		help += " Default: " + s.def + "."
	}

	app.Flag(s.names[0], help).StringVar(s.dst)
	for _, alias := range s.names[1:] {
		app.Flag(alias, "Alias for --"+s.names[0]+".").Hidden().StringVar(s.dst)
	}
}

func (s stringSetting) resolve() {
	if *s.dst != "" {
		return
	}
	for _, env := range s.envs {
		if v := os.Getenv(env); v != "" {
			*s.dst = v
			return
		}
	}
	*s.dst = s.def
}

func (c *Config) stringSettings() []stringSetting {
	return []stringSetting{
		{
			dst:   &c.Address,
			help:  "Address of the Loki server.",
			def:   defaultAddress,
			envs:  []string{"LOKI_ADDR", "LOKI_ADDRESS"},
			names: []string{"address", "addr"},
		},
		{
			dst:   &c.OrgID,
			help:  "Tenant ID, sent in the X-Scope-OrgID header. Useful for requesting tenant data when bypassing an auth gateway.",
			envs:  []string{"LOKI_ORG_ID", "LOKI_TENANT_ID"},
			names: []string{"org-id", "id"},
		},
		{
			dst:   &c.Username,
			help:  "Username for HTTP basic auth.",
			envs:  []string{"LOKI_USERNAME", "LOKI_API_USER"},
			names: []string{"username", "user"},
		},
		{
			dst:   &c.Password,
			help:  "Password for HTTP basic auth. If no username is set, the org ID is used as username.",
			envs:  []string{"LOKI_PASSWORD", "LOKI_API_KEY"},
			names: []string{"password", "pass"},
		},
		{
			dst:   &c.BearerToken,
			help:  "Bearer token for authentication.",
			envs:  []string{"LOKI_BEARER_TOKEN", "LOKI_AUTH_TOKEN"},
			names: []string{"bearer-token", "authToken"},
		},
		{
			dst:   &c.TLS.CAFile,
			help:  "Path to the certificate authority of the server.",
			envs:  []string{"LOKI_CA_CERT_PATH", "LOKI_TLS_CA_PATH"},
			names: []string{"tls-ca-path", "ca-cert", "ca-cert-path"},
		},
		{
			dst:   &c.TLS.CertFile,
			help:  "Path to the client certificate for mutual TLS.",
			envs:  []string{"LOKI_CLIENT_CERT_PATH", "LOKI_TLS_CERT_PATH"},
			names: []string{"tls-cert-path", "cert", "cert-path"},
		},
		{
			dst:   &c.TLS.KeyFile,
			help:  "Path to the private key of the client certificate for mutual TLS.",
			envs:  []string{"LOKI_CLIENT_KEY_PATH", "LOKI_TLS_KEY_PATH"},
			names: []string{"tls-key-path", "key", "key-path"},
		},
	}
}

// Register the connection flags with the kingpin application.
// Call Resolve after parsing to apply environment variables and defaults.
func (c *Config) Register(app *kingpin.Application) {
	for _, s := range c.stringSettings() {
		s.register(app)
	}

	app.Flag("bearer-token-file", "Path to a file that contains the bearer token. Can also be set using LOKI_BEARER_TOKEN_FILE env var.").
		Envar("LOKI_BEARER_TOKEN_FILE").StringVar(&c.BearerTokenFile)
	app.Flag("auth-header", "Name of the header that carries the credentials. Can also be set using LOKI_AUTH_HEADER env var.").
		Default(defaultAuthHeader).Envar("LOKI_AUTH_HEADER").StringVar(&c.AuthHeader)
	app.Flag("tls-skip-verify", "Skip verification of the server certificate. Can also be set using LOKI_TLS_SKIP_VERIFY env var.").
		Envar("LOKI_TLS_SKIP_VERIFY").BoolVar(&c.TLS.InsecureSkipVerify)
	app.Flag("query-tags", "Value of the X-Query-Tags header. The value is part of the query statistics in the Loki logs. Can also be set using LOKI_QUERY_TAGS env var.").
		Envar("LOKI_QUERY_TAGS").StringVar(&c.QueryTags)
	app.Flag("nocache", "Send a Cache-Control: no-cache header. Can also be set using LOKI_NO_CACHE env var.").
		Envar("LOKI_NO_CACHE").BoolVar(&c.NoCache)
	app.Flag("retries", "Number of times a request is retried when it fails. Can also be set using LOKI_CLIENT_RETRIES env var.").
		Default("0").Envar("LOKI_CLIENT_RETRIES").IntVar(&c.Retries)
	app.Flag("min-backoff", "Minimum wait between retries in seconds. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.").
		Default("0").Envar("LOKI_CLIENT_MIN_BACKOFF").IntVar(&c.MinBackoff)
	app.Flag("max-backoff", "Maximum wait between retries in seconds. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.").
		Default("0").Envar("LOKI_CLIENT_MAX_BACKOFF").IntVar(&c.MaxBackoff)
	app.Flag("proxy-url", "HTTP or HTTPS proxy for all requests. Takes precedence over --envproxy. Can also be set using LOKI_HTTP_PROXY_URL env var.").
		Envar("LOKI_HTTP_PROXY_URL").StringVar(&c.ProxyURL)
	app.Flag("envproxy", "Use the proxy settings of the environment (HTTP_PROXY, HTTPS_PROXY, NO_PROXY). Can also be set using LOKI_ENV_PROXY env var.").
		Default("true").Envar("LOKI_ENV_PROXY").BoolVar(&c.EnvProxy)
	app.Flag("compress", "Request compression of responses. Can also be set using LOKI_HTTP_COMPRESSION env var.").
		Envar("LOKI_HTTP_COMPRESSION").BoolVar(&c.Compress)
	app.Flag("header", "Add a custom HTTP header to requests. Can be specified multiple times. Format: 'Header-Name: value'").
		StringsVar(&c.Headers)
}

// Resolve applies environment variables and defaults to the settings that
// are not set. The order of precedence is: flag, environment variables in the
// order of their documentation, default.
func (c *Config) Resolve() {
	for _, s := range c.stringSettings() {
		s.resolve()
	}
}

// Validate checks the combination of settings. It must be called after Resolve.
func (c *Config) Validate() error {
	basicAuth := c.Username != "" || c.Password != ""
	bearerAuth := c.BearerToken != "" || c.BearerTokenFile != ""

	if basicAuth && bearerAuth {
		return fmt.Errorf("at most one of HTTP basic auth (username/password), bearer-token and bearer-token-file is allowed to be configured")
	}
	if c.BearerToken != "" && c.BearerTokenFile != "" {
		return fmt.Errorf("at most one of the options bearer-token and bearer-token-file is allowed to be configured")
	}
	if c.Password != "" && c.Username == "" && c.OrgID == "" {
		return fmt.Errorf("password requires a username or org-id")
	}
	return nil
}
