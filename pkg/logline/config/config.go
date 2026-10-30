// Package config holds the top-level logline configuration.
package config

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v4/pkg/logline"
	"github.com/grafana/loki/v4/pkg/logline/builder"
	"github.com/grafana/loki/v4/pkg/logline/correctness"
	"github.com/grafana/loki/v4/pkg/logline/queryfrontend"
	"github.com/grafana/loki/v4/pkg/logline/store"
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

	// Correctness is the logline correctness config.
	Correctness correctness.Config `yaml:"correctness"`

	// Compaction merges indexes into fewer, larger ones.
	Compaction logline.CompactionConfig `yaml:"compaction"`

	// Retention decides when indexes are deleted.
	Retention logline.RetentionConfig `yaml:"retention"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Index.RegisterFlagsWithPrefix("logline-index", f)
	cfg.Store.RegisterFlags(f)
	cfg.Builder.RegisterFlagsWithPrefix("logline-builder", f)
	cfg.Query.RegisterFlagsWithPrefix("logline-query", f)
	cfg.Correctness.RegisterFlagsWithPrefix("logline-correctness", f)
	cfg.Compaction.RegisterFlagsWithPrefix("logline-compaction", f)
	cfg.Retention.RegisterFlagsWithPrefix("logline-retention", f)
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

// ValidateQueryConfig checks the config logline filtering in the query path needs.
// It is a no-op unless the query section is enabled.
func (cfg *Config) ValidateQueryConfig() error {
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

// ValidateCorrectness checks the config the correctness target needs.
//
// Correctness.QueryIngestersWithin must already hold a copy of
// querier.query_ingesters_within. NgramLength is filled at construction in
// initLoglineCorrectness and Store.QueryIngestersWithin in loglineStoreConfig,
// not here. Validation
// applies defaults as a side effect, so it must run before the config is used.
func (cfg *Config) ValidateCorrectness() error {
	if err := cfg.Index.Validate(); err != nil {
		return fmt.Errorf("invalid index config: %w", err)
	}
	if err := cfg.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config: %w", err)
	}
	return cfg.Correctness.Validate()
}

// ValidateScheduler checks the config the compaction scheduler needs: it plans
// and hands out compaction jobs and deletes indexes. Validation applies
// defaults as a side effect, so it must run before the config is used.
func (cfg *Config) ValidateScheduler() error {
	if err := cfg.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config: %w", err)
	}
	if err := cfg.Compaction.ValidateScheduler(); err != nil {
		return fmt.Errorf("invalid compaction config: %w", err)
	}
	if err := cfg.Retention.Validate(); err != nil {
		return fmt.Errorf("invalid retention config: %w", err)
	}
	return nil
}

// ValidateCompactor checks the config the compactor needs to run merge
// jobs. Validation applies defaults as a side effect, so it must run before
// the config is used.
func (cfg *Config) ValidateCompactor() error {
	if err := cfg.Index.Validate(); err != nil {
		return fmt.Errorf("invalid index config: %w", err)
	}
	if err := cfg.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config: %w", err)
	}
	if err := cfg.Compaction.ValidateCompactor(); err != nil {
		return fmt.Errorf("invalid compaction config: %w", err)
	}
	return nil
}
