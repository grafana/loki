package logline

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/shard"
)

const (
	DefaultNgramLength      = 6
	DefaultDocumentInterval = 100 * time.Millisecond

	// DefaultDensityThreshold is the v3 format default: n-grams present in
	// more than 20% of a full day's documents are stored as match-all.
	DefaultDensityThreshold = 0.20

	// maxNgramLength is the longest n-gram the extractors produce.
	maxNgramLength = 8
)

// IndexConfig holds the index config for logline.
type IndexConfig struct {
	// Version is the index format version written to meta.json. Readers use
	// the version recorded in each index, so this only governs writers.
	Version     string `yaml:"version"`
	NgramLength int    `yaml:"ngram_length"`
	// DocumentInterval is the time range one document covers.
	DocumentInterval time.Duration `yaml:"document_interval"`
	// DensityThreshold is the fraction of a full day's documents above which
	// an n-gram is stored as match-all. 0 selects the format default.
	DensityThreshold float64 `yaml:"density_threshold"`
	// ShardCount is the number of n-gram shards per date. 0 or 1 disables sharding.
	ShardCount     int    `yaml:"shard_count"`
	ShardAlgorithm string `yaml:"shard_algorithm"`
}

func (c *IndexConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Version, prefix+".version", "",
		"Index format version written to meta.json. Defaults to the current version.")
	f.IntVar(&c.NgramLength, prefix+".ngram-length", DefaultNgramLength,
		"N-gram length used to build and to query the index. It is not recorded in the index, so every component must use the same value.")
	f.DurationVar(&c.DocumentInterval, prefix+".document-interval", DefaultDocumentInterval,
		"Time range each document covers, for example 100ms or 1s.")
	f.Float64Var(&c.DensityThreshold, prefix+".density-threshold", 0,
		"Store n-grams covering more than this fraction of a full day's documents as match-all. 0 uses the format default (v3: 0.20).")
	f.IntVar(&c.ShardCount, prefix+".shard-count", 0,
		"Number of n-gram shards per date. 0 or 1 disables sharding (one file per date).")
	f.StringVar(&c.ShardAlgorithm, prefix+".shard-algorithm", "murmur3_mix",
		"Shard algorithm for n-gram routing. Valid values: first_byte, murmur3_mix.")
}

// Validate applies the format defaults and checks the format constraints. It
// mutates the receiver, so it must run before the config is used.
func (c *IndexConfig) Validate() error {
	if c.NgramLength == 0 {
		c.NgramLength = DefaultNgramLength
	}
	if c.DocumentInterval == 0 {
		c.DocumentInterval = DefaultDocumentInterval
	}
	if c.Version == "" {
		c.Version = CurrentVersion
	}
	if c.DensityThreshold == 0 {
		c.DensityThreshold = DefaultDensityThreshold
	}

	if c.NgramLength < 1 || c.NgramLength > maxNgramLength {
		return fmt.Errorf("ngram_length must be between 1 and %d, got %d", maxNgramLength, c.NgramLength)
	}
	if c.DocumentInterval < 0 {
		return fmt.Errorf("document_interval must be positive, got %v", c.DocumentInterval)
	}
	if err := ValidateVersion(c.Version); err != nil {
		return fmt.Errorf("invalid index version: %w", err)
	}
	if c.ShardCount < 0 {
		return fmt.Errorf("shard_count must be >= 0, got %d", c.ShardCount)
	}
	if c.ShardCount > 1 {
		if c.ShardAlgorithm == "" {
			return fmt.Errorf("shard_algorithm must be set when shard_count > 1")
		}
		if _, err := shard.New(c.ShardAlgorithm); err != nil {
			return fmt.Errorf("invalid shard config: %w", err)
		}
	}
	return nil
}
