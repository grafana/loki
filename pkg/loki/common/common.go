package common

// Config holds common config that can be shared between multiple other config sections
type Config struct {
	PathPrefix string `yaml:"path_prefix"`
}
