// Package config holds the top-level logline configuration.
package config

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

const (
	defaultNgramLength     = 6
	defaultMaxHintParallel = 64
)

// Config is the logline section of Loki's config.
//
// k325 does not have the builder or the main-line queryfrontend.Config split,
// so this is the querier-facing subset the hint-job path needs.
type Config struct {
	// Index is the index shape used by querier hint lookups.
	Index IndexConfig `yaml:"index"`

	// Store addresses the index in object storage.
	Store store.Config `yaml:"store"`

	// Query is the logline read path config.
	Query QueryConfig `yaml:"query"`
}

// IndexConfig is the querier-facing index shape. NgramLength is not recorded
// in the index, so every component must use the same value the builder did.
type IndexConfig struct {
	NgramLength int `yaml:"ngram_length"`
}

// QueryConfig enables querier hint jobs and their index-worker parallelism.
type QueryConfig struct {
	Enabled         bool `yaml:"enabled"`
	MaxHintParallel int  `yaml:"max_hint_parallel"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Index.RegisterFlagsWithPrefix("logline-index", f)
	cfg.Store.RegisterFlags(f)
	cfg.Query.RegisterFlagsWithPrefix("logline-query", f)
}

func (c *IndexConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&c.NgramLength, prefix+".ngram-length", defaultNgramLength,
		"N-gram length used to query the index. It is not recorded in the index, so every component must use the same value.")
}

func (c *IndexConfig) Validate() error {
	if c.NgramLength <= 0 {
		c.NgramLength = defaultNgramLength
	}
	return nil
}

func (c *QueryConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, prefix+".enabled", false,
		"Enable logline hint lookups on queriers.")
	f.IntVar(&c.MaxHintParallel, prefix+".max-hint-parallel", 0,
		"Maximum concurrent hint index workers per query (default 64)")
}

func (c *QueryConfig) Validate() error {
	if c.MaxHintParallel <= 0 {
		c.MaxHintParallel = defaultMaxHintParallel
	}
	return nil
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
