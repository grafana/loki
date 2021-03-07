package config

import (
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/pkg/errors"
)

type Config struct {
	ruler.Config `yaml:",inline"`

	RemoteWriteConfig `yaml:"remote_write"`
}

type RemoteWriteConfig struct {
	URL string `yaml:"url"`
}

// Override the embedded cortex variant which expects a cortex limits struct. Instead copy the relevant bits over.
func (cfg *Config) Validate() error {
	if err := cfg.StoreConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	return nil
}
