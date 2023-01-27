package wal

import (
	"time"
)

const (
	defaultMaxSegmentAge = time.Hour
)

// Config contains all WAL-related settings.
type Config struct {
	// Path where the WAL is written to.
	Dir string `yaml:"dir"`

	// Whether WAL-support should be enabled.
	//
	// WAL support is a WIP. Do not enable in production setups until https://github.com/grafana/loki/issues/8197
	// is finished.
	Enabled bool `yaml:"enabled"`

	// MaxSegmentAge is threshold at which a WAL segment is considered old enough to be cleaned up. Default: 1h.
	MaxSegmentAge time.Duration `yaml:"cleanSegmentsOlderThan"`
}

// UnmarshalYAML implement YAML Unmarshaler
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Apply defaults
	c.MaxSegmentAge = defaultMaxSegmentAge
	type plain Config
	return unmarshal((*plain)(c))
}
