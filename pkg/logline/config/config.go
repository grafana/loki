// Package config holds the top-level logline configuration, the single
// "logline" section of Loki's config file.
package config

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/logline/builder"
	"github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Config is the "logline" section of Loki's config.
//
// There is deliberately no Enabled field. Selecting the target is the gate:
// no logline module is in the All target and nothing else depends on them, so
// they are only ever constructed when a logline target is named explicitly.
// Validate is likewise called only for the selected target, so a deployment
// that never mentions logline neither validates nor runs any of it.
//
// Note also that -logline.enabled is already taken: the query frontend
// middleware registers it as its read-path off-switch, and cells set it today.
type Config struct {
	// Store is shared by every logline component, so it lives here rather
	// than under any one of them.
	Store store.Config `yaml:"store"`

	// IndexBuilder is the logline-index-builder target's configuration.
	IndexBuilder builder.Config `yaml:"index_builder"`

	// QueryFrontend configures the read path: the middleware injected into
	// the query frontend chain. It carries its own Enabled field, because
	// unlike the builder the read path is not selected by target.
	QueryFrontend queryfrontend.Config `yaml:"query_frontend"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Store.RegisterFlags(f)
	cfg.IndexBuilder.RegisterFlags(f)
	cfg.QueryFrontend.RegisterFlags(f)
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
