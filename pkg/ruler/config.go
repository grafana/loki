package ruler

import (
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
)

type Config struct {
	ruler.Config `yaml:",inline"`

	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Config.RegisterFlags(f)
	c.RemoteWrite.RegisterFlags(f)

	// TODO(owen-d, 3.0.0): remove deprecated experimental prefix in Cortex if they'll accept it.
	f.BoolVar(&c.Config.EnableAPI, "ruler.enable-api", false, "Enable the ruler api")
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
	Client  config.RemoteWriteConfig `yaml:"client"`
	Enabled bool                     `yaml:"enabled"`
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
}
