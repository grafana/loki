package ruler

import (
	"flag"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"

	"github.com/grafana/loki/pkg/ruler/storage/instance"
)

type Config struct {
	ruler.Config `yaml:",inline"`

	WAL instance.Config `yaml:"wal,omitempty"`
	// we cannot define this in the WAL config since it creates an import cycle

	// TODO(dannyk): once we have a way to know when a rulegroup has been unregistered,
	// 				 we can enable the WAL cleaner - which cleans up WALs that are no longer managed
	//WALCleaner  cleaner.Config    `yaml:"wal_cleaner,omitempty"`
	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Config.RegisterFlags(f)
	c.RemoteWrite.RegisterFlags(f)

	// TODO(owen-d, 3.0.0): remove deprecated experimental prefix in Cortex if they'll accept it.
	f.BoolVar(&c.Config.EnableAPI, "ruler.enable-api", false, "Enable the ruler api")

	f.StringVar(&c.WAL.Dir, "ruler.wal.dir", instance.DefaultConfig.Dir, "Directory to store the WAL and/or recover from WAL.")
	f.DurationVar(&c.WAL.TruncateFrequency, "ruler.wal.truncate-frequency", instance.DefaultConfig.TruncateFrequency, "How often to run the WAL truncation.")
	f.DurationVar(&c.WAL.MinAge, "ruler.wal.min-age", instance.DefaultConfig.MinAge, "Minimum age that samples must exist in the WAL before being truncated.")
	f.DurationVar(&c.WAL.MaxAge, "ruler.wal.max-age", instance.DefaultConfig.MaxAge, "Maximum age that samples must exist in the WAL before being truncated.")
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

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (c *RemoteWriteConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "ruler.remote-write.enabled", false, "Remote-write recording rule samples to Prometheus-compatible remote-write receiver.")
	f.DurationVar(&c.ConfigRefreshPeriod, "ruler.remote-write.config-refresh-period", 10*time.Second, "Minimum period to wait between remote-write reconfigurations. This should be greater than or equivalent to -limits.per-user-override-period.")
}
