package instance

import "github.com/prometheus/prometheus/config"

// DefaultGlobalConfig holds default global settings to be used across all instances.
var DefaultGlobalConfig = GlobalConfig{
	Prometheus: config.DefaultGlobalConfig,
}

// GlobalConfig holds global settings that apply to all instances by default.
type GlobalConfig struct {
	Prometheus  config.GlobalConfig         `yaml:",inline"`
	RemoteWrite []*config.RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *GlobalConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultGlobalConfig

	type plain GlobalConfig
	return unmarshal((*plain)(c))
}
