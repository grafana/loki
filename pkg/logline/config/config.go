// Package config holds the top-level logline configuration.
//
// It is the "logline" section of Loki's config file in shape only: loki.Config
// currently tags the field `yaml:"-"` and the settings are supplied by flags.
// The yaml tags below are kept so the section is ready to be exposed; see the
// TODO on loki.Config.Logline.
package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/logline/builder"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Config is the logline section of Loki's config, configured by flags until
// the yaml tag on loki.Config.Logline is restored.
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
