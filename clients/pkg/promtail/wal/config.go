package wal

type Config struct {
	Dir     string `yaml:"dir"`
	Enabled bool   `yaml:"enabled"`
}
