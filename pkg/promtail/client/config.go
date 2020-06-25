package client

import (
	"flag"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/config"

	lokiflag "github.com/grafana/loki/pkg/util/flagext"
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

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(flags *flag.FlagSet) {
	flags.Var(&c.URL, "client.url", "URL of log server")
	flags.DurationVar(&c.BatchWait, "client.batch-wait", 1*time.Second, "Maximum wait period before sending batch.")
	flags.IntVar(&c.BatchSize, "client.batch-size-bytes", 100*1024, "Maximum batch size to accrue before sending. ")
	// Default backoff schedule: 0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(4.267m) For a total time of 511.5s(8.5m) before logs are lost
	flag.IntVar(&c.BackoffConfig.MaxRetries, "client.max-retries", 10, "Maximum number of retires when sending batches.")
	flag.DurationVar(&c.BackoffConfig.MinBackoff, "client.min-backoff", 500*time.Millisecond, "Initial backoff time between retries.")
	flag.DurationVar(&c.BackoffConfig.MaxBackoff, "client.max-backoff", 5*time.Minute, "Maximum backoff time between retries.")
	flag.DurationVar(&c.Timeout, "client.timeout", 10*time.Second, "Maximum time to wait for server to respond to a request")
	flags.Var(&c.ExternalLabels, "client.external-labels", "list of external labels to add to each log (e.g: --client.external-labels=lb1=v1,lb2=v2)")

	flags.StringVar(&c.TenantID, "client.tenant-id", "", "Tenant ID to use when pushing logs to Loki.")
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
				MaxBackoff: 5 * time.Minute,
				MaxRetries: 10,
				MinBackoff: 500 * time.Millisecond,
			},
			BatchSize: 100 * 1024,
			BatchWait: 1 * time.Second,
			Timeout:   10 * time.Second,
		}
	}

	if err := unmarshal(&cfg); err != nil {
		return err
	}

	*c = Config(cfg)
	return nil
}
