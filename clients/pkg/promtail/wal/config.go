package wal

// Config contains all WAL-related settings.
type Config struct {
	// Path where the WAL is written to.
	Dir string `yaml:"dir"`

	// Whether WAL-support should be enabled.
	Enabled bool `yaml:"enabled"`
}
