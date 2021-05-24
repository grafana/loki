package ruler

import (
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
)

// DefaultBufferSize defines the default size of the samples buffer which will hold samples
// while the remote-write endpoint is unavailable
const DefaultBufferSize = 100

type Config struct {
	ruler.Config `yaml:",inline"`

	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

type RemoteWriteConfig struct {
	Client config.RemoteWriteConfig `yaml:"client"`

	BufferSize int `yaml:"buffer_size,omitempty"`
}

func (c *RemoteWriteConfig) Enabled() bool {
	// remote-write is considered disabled if there's no target to write to
	return c.Client.URL != nil
}

// Validate overrides the embedded cortex variant which expects a cortex limits struct. Instead copy the relevant bits over.
func (cfg *Config) Validate() error {
	if err := cfg.StoreConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid ruler config")
	}
	return nil
}
