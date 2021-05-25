package ruler

import (
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

type RemoteWriteConfig struct {
	Client  config.RemoteWriteConfig `yaml:"client"`
	Enabled bool                     `yaml:"enabled"`

	QueueCapacity int `yaml:"queue_capacity,omitempty"`
}

func (c *RemoteWriteConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type raw RemoteWriteConfig
	var cfg raw

	// set defaults
	cfg.QueueCapacity = DefaultQueueCapacity

	if err := unmarshal(&cfg); err != nil {
		return err
	}

	*c = RemoteWriteConfig(cfg)
	return nil
}

// Validate overrides the embedded cortex variant which expects a cortex limits struct. Instead copy the relevant bits over.
func (cfg *Config) Validate() error {
	if err := cfg.StoreConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid ruler config")
	}
	return nil
}
