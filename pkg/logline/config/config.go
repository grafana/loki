// Package config holds the top-level logline configuration.
package config

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/builder"
	"github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Config is the logline section of Loki's config.
type Config struct {
	// Index is the index shape.
	Index logline.IndexConfig `yaml:"index"`

	// Store addresses the index in object storage.
	Store store.Config `yaml:"store"`

	// Builder is the index building config.
	Builder builder.Config `yaml:"builder"`

	// Query is the logline read path config.
	Query queryfrontend.Config `yaml:"query"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Index.RegisterFlagsWithPrefix("logline-index", f)
	cfg.Store.RegisterFlags(f)
	cfg.Builder.RegisterFlagsWithPrefix("logline-builder", f)
	cfg.Query.RegisterFlagsWithPrefix("logline-query", f)
}

// ValidateBuilder checks the config index building needs.
//
// Builder.Index must already hold a copy of Index. Validation applies defaults
// as a side effect, so it must run before the config is used.
func (cfg *Config) ValidateBuilder() error {
	if err := cfg.Index.Validate(); err != nil {
		return fmt.Errorf("invalid index config: %w", err)
	}
	if err := cfg.Store.Validate(); err != nil {
		return err
	}
	return cfg.Builder.Validate()
}

// ValidateQuery checks the config logline filtering in the query path needs.
// It is a no-op unless the query section is enabled.
func (cfg *Config) ValidateQuery() error {
	if !cfg.Query.Enabled {
		return nil
	}
	if err := cfg.Index.Validate(); err != nil {
		return fmt.Errorf("invalid index config: %w", err)
	}
	if err := cfg.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config: %w", err)
	}
	if err := cfg.Query.Validate(); err != nil {
		return fmt.Errorf("invalid query config: %w", err)
	}
	return nil
}
