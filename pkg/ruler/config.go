package ruler

import (
	"flag"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
	"gopkg.in/yaml.v2"

	ruler "github.com/grafana/loki/pkg/ruler/base"
	"github.com/grafana/loki/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/pkg/ruler/storage/instance"
)

type Config struct {
	ruler.Config `yaml:",inline"`

	WAL instance.Config `yaml:"wal,omitempty"`
	// we cannot define this in the WAL config since it creates an import cycle

	WALCleaner  cleaner.Config    `yaml:"wal_cleaner,omitempty"`
	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Config.RegisterFlags(f)
	c.RemoteWrite.RegisterFlags(f)
	c.WAL.RegisterFlags(f)
	c.WALCleaner.RegisterFlags(f)

	// TODO(owen-d, 3.0.0): remove deprecated experimental prefix in Cortex if they'll accept it.
	f.BoolVar(&c.Config.EnableAPI, "ruler.enable-api", true, "Enable the ruler api")
}

// Validate overrides the embedded cortex variant which expects a cortex limits struct. Instead copy the relevant bits over.
func (c *Config) Validate() error {
	if err := c.StoreConfig.Validate(); err != nil {
		return fmt.Errorf("invalid ruler store config: %w", err)
	}

	if err := c.RemoteWrite.Validate(); err != nil {
		return fmt.Errorf("invalid ruler remote-write config: %w", err)
	}

	return nil
}

type RemoteWriteConfig struct {
	Client              config.RemoteWriteConfig `yaml:"client"`
	Enabled             bool                     `yaml:"enabled"`
	ConfigRefreshPeriod time.Duration            `yaml:"config_refresh_period"`
}

func (c *RemoteWriteConfig) Validate() error {
	if c.Enabled && c.Client.URL == nil {
		return errors.New("remote-write enabled but client URL is not configured")
	}

	return nil
}

func (c *RemoteWriteConfig) Clone() (*RemoteWriteConfig, error) {
	out, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}

	var n *RemoteWriteConfig
	err = yaml.Unmarshal(out, &n)
	if err != nil {
		return nil, err
	}

	// BasicAuth.Password has a type of Secret (github.com/prometheus/common/config/config.go),
	// so when its value is marshaled it is obfuscated as "<secret>".
	// Here we copy the original password into the cloned config.
	if n.Client.HTTPClientConfig.BasicAuth != nil {
		n.Client.HTTPClientConfig.BasicAuth.Password = c.Client.HTTPClientConfig.BasicAuth.Password
	}
	return n, nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (c *RemoteWriteConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "ruler.remote-write.enabled", false, "Remote-write recording rule samples to Prometheus-compatible remote-write receiver.")
	f.DurationVar(&c.ConfigRefreshPeriod, "ruler.remote-write.config-refresh-period", 10*time.Second, "Minimum period to wait between refreshing remote-write reconfigurations. This should be greater than or equivalent to -limits.per-user-override-period.")
}
