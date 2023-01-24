package wal

// Config contains all WAL-related settings.
type Config struct {
	// Path where the WAL is written to.
	Dir string `yaml:"dir"`

	// Whether WAL-support should be enabled.
	//
	// WAL support is a WIP. Do not enable in production setups until https://github.com/grafana/loki/issues/8197
	// is finished.
	Enabled bool `yaml:"enabled"`
}
