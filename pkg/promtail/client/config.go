package client

import (
	"flag"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/config"

	lokiflag "github.com/grafana/loki/pkg/util/flagext"
)

// NOTE the helm chart for promtail and fluent-bit also have defaults for these values, please update to match if you make changes here.
const (
	BatchWait      = 1 * time.Second
	BatchSize  int = 1024 * 1024
	MinBackoff     = 500 * time.Millisecond
	MaxBackoff     = 5 * time.Minute
	MaxRetries int = 10
	Timeout        = 10 * time.Second
)

// Config describes configuration for a HTTP pusher client.
type Config struct {
	URL       flagext.URLValue
	BatchWait time.Duration
	BatchSize int

	Client config.HTTPClientConfig `yaml:",inline"`

	BackoffConfig util.BackoffConfig `yaml:"backoff_config"`
	// The labels to add to any time series or alerts when communicating with loki
	ExternalLabels lokiflag.LabelSet `yaml:"external_labels,omitempty"`
	Timeout        time.Duration     `yaml:"timeout"`

	// The tenant ID to use when pushing logs to Loki (empty string means
	// single tenant mode)
	TenantID string `yaml:"tenant_id"`
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&c.URL, prefix+"client.url", "URL of log server")
	f.DurationVar(&c.BatchWait, prefix+"client.batch-wait", BatchWait, "Maximum wait period before sending batch.")
	f.IntVar(&c.BatchSize, prefix+"client.batch-size-bytes", BatchSize, "Maximum batch size to accrue before sending. ")
	// Default backoff schedule: 0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(4.267m) For a total time of 511.5s(8.5m) before logs are lost
	f.IntVar(&c.BackoffConfig.MaxRetries, prefix+"client.max-retries", MaxRetries, "Maximum number of retires when sending batches.")
	f.DurationVar(&c.BackoffConfig.MinBackoff, prefix+"client.min-backoff", MinBackoff, "Initial backoff time between retries.")
	f.DurationVar(&c.BackoffConfig.MaxBackoff, prefix+"client.max-backoff", MaxBackoff, "Maximum backoff time between retries.")
	f.DurationVar(&c.Timeout, prefix+"client.timeout", Timeout, "Maximum time to wait for server to respond to a request")
	f.Var(&c.ExternalLabels, prefix+"client.external-labels", "list of external labels to add to each log (e.g: --client.external-labels=lb1=v1,lb2=v2)")

	f.StringVar(&c.TenantID, prefix+"client.tenant-id", "", "Tenant ID to use when pushing logs to Loki.")
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(flags *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", flags)
}

// UnmarshalYAML implement Yaml Unmarshaler
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type raw Config
	var cfg raw
	if c.URL.URL != nil {
		// we used flags to set that value, which already has sane default.
		cfg = raw(*c)
	} else {
		// force sane defaults.
		cfg = raw{
			BackoffConfig: util.BackoffConfig{
				MaxBackoff: MaxBackoff,
				MaxRetries: MaxRetries,
				MinBackoff: MinBackoff,
			},
			BatchSize: BatchSize,
			BatchWait: BatchWait,
			Timeout:   Timeout,
		}
	}

	if err := unmarshal(&cfg); err != nil {
		return err
	}

	*c = Config(cfg)
	return nil
}
