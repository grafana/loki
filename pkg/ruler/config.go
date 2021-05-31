package ruler

import (
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
)

// DefaultQueueCapacity defines the default size of the samples queue which will hold samples
// while the remote-write endpoint is unavailable
const DefaultQueueCapacity = 10000

type Config struct {
	ruler.Config `yaml:",inline"`

	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Config.RegisterFlags(f)
	c.RemoteWrite.RegisterFlags(f)
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

	QueueCapacity int `yaml:"queue_capacity,omitempty"`
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
	f.IntVar(&c.QueueCapacity, "ruler.remote-write.queue-capacity", DefaultQueueCapacity, "Capacity of remote-write queues; if a queue exceeds its capacity it will evict oldest samples.")
}
