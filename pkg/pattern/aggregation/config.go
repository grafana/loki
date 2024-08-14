package aggregation

import (
	"flag"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
)

type Config struct {
	// TODO(twhitney): This needs to be a per-tenant config
	Enabled          bool                    `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester metric aggregation is enabled."`
	DownsamplePeriod time.Duration           `yaml:"downsample_period"`
	LokiAddr         string                  `yaml:"loki_address,omitempty" doc:"description=The address of the Loki instance to push aggregated metrics to."`
	WriteTimeout     time.Duration           `yaml:"timeout,omitempty" doc:"description=The timeout for writing to Loki."`
	PushPeriod       time.Duration           `yaml:"push_period,omitempty" doc:"description=How long to wait in between pushes to Loki."`
	HTTPClientConfig config.HTTPClientConfig `yaml:"http_client_config,omitempty" doc:"description=The HTTP client configuration for pushing metrics to Loki."`
	UseTLS           bool                    `yaml:"use_tls,omitempty" doc:"description=Whether to use TLS for pushing metrics to Loki."`
	BasicAuth        BasicAuth               `yaml:"basic_auth,omitempty" doc:"description=The basic auth configuration for pushing metrics to Loki."`
	BackoffConfig    backoff.Config          `yaml:"backoff_config,omitempty" doc:"description=The backoff configuration for pushing metrics to Loki."`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(fs, "")
}

func (cfg *Config) RegisterFlagsWithPrefix(fs *flag.FlagSet, prefix string) {
	fs.BoolVar(
		&cfg.Enabled,
		prefix+"metric-aggregation.enabled",
		false,
		"Flag to enable or disable metric aggregation.",
	)
	fs.DurationVar(
		&cfg.DownsamplePeriod,
		prefix+"metric-aggregation.downsample-period",
		10*time.Second,
		"How often to downsample metrics from raw push observations.",
	)
	fs.StringVar(
		&cfg.LokiAddr,
		prefix+"metric-aggregation.loki-address",
		"",
		"Loki address to send aggregated metrics to.",
	)
	fs.DurationVar(
		&cfg.WriteTimeout,
		prefix+"metric-aggregation.timeout",
		10*time.Second,
		"How long to wait write response from Loki",
	)
	fs.DurationVar(
		&cfg.PushPeriod,
		prefix+"metric-aggregation.push-period",
		30*time.Second,
		"How long to wait write response from Loki",
	)
	fs.BoolVar(
		&cfg.UseTLS,
		prefix+"metric-aggregation.tls",
		false,
		"Does the loki connection use TLS?",
	)

	cfg.BackoffConfig.RegisterFlagsWithPrefix(prefix+"metric-aggregation", fs)
	cfg.BasicAuth.RegisterFlagsWithPrefix(prefix+"metric-aggregation.", fs)
}

// BasicAuth contains basic HTTP authentication credentials.
type BasicAuth struct {
	Username string `yaml:"username"           json:"username"`
	// UsernameFile string `yaml:"username_file,omitempty" json:"username_file,omitempty"`
	Password config.Secret `yaml:"password,omitempty" json:"password,omitempty"`
	// PasswordFile string `yaml:"password_file,omitempty" json:"password_file,omitempty"`
}

func (cfg *BasicAuth) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.StringVar(
		&cfg.Username,
		prefix+"basic-auth.username",
		"",
		"Basic auth username for sending aggregations back to Loki.",
	)
	fs.Var(
		newSecretValue(config.Secret(""), &cfg.Password),
		prefix+"basic-auth.password",
		"Basic auth password for sending aggregations back to Loki.",
	)
}

type secretValue string

func newSecretValue(val config.Secret, p *config.Secret) *secretValue {
	*p = val
	return (*secretValue)(p)
}

func (s *secretValue) Set(val string) error {
	*s = secretValue(val)
	return nil
}

func (s *secretValue) Get() any { return string(*s) }

func (s *secretValue) String() string { return string(*s) }
