// Package config holds the top-level logline configuration, the single
// "logline" section of Loki's config file.
package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/logline/builder"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Config is the "logline" section of Loki's config.
type Config struct {
	// Store is shared by every logline component, so it lives here rather
	// than under any one of them.
	Store store.Config `yaml:"store"`

	// IndexBuilder is the logline-index-builder target's configuration.
	IndexBuilder builder.Config `yaml:"index_builder"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Store.RegisterFlags(f)
	cfg.IndexBuilder.RegisterFlags(f)
}

// ValidateIndexBuilder checks the config the logline-index-builder target needs.
//
// The builder's own Validate applies defaults as a side effect, so it must run
// before the config is used.
func (cfg *Config) ValidateIndexBuilder() error {
	if err := cfg.Store.Validate(); err != nil {
		return err
	}
	return cfg.IndexBuilder.Validate()
}
