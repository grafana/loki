package filesystem

import "flag"

// Config stores the configuration for storing and accessing objects in the local filesystem.
type Config struct {
	Directory string `yaml:"dir"`
}

// RegisterFlags registers the flags for TSDB filesystem storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, "experimental.tsdb.filesystem.dir", "", "Local filesystem storage directory.")
}
