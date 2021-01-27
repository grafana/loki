package filesystem

import "flag"

// Config stores the configuration for storing and accessing objects in the local filesystem.
type Config struct {
	Directory string `yaml:"dir"`
}

// RegisterFlags registers the flags for filesystem storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for filesystem storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, prefix+"filesystem.dir", "", "Local filesystem storage directory.")
}
