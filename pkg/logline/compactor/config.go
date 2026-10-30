package compactor

import (
	"fmt"

	"github.com/grafana/loki/v4/pkg/logline"
)

// Config is what the compactor reads from the logline config.
type Config struct {
	Compaction logline.CompactionConfig

	// DensityThreshold is logline.index.density_threshold. Merged indexes
	// store n-grams covering more than this fraction of a full day's documents
	// as match-all, the same as the builder does. 0 selects the format default.
	DensityThreshold float64
}

// Validate applies defaults for zero values and checks the config.
func (c *Config) Validate() error {
	if err := c.Compaction.ValidateCompactor(); err != nil {
		return fmt.Errorf("invalid compaction config: %w", err)
	}
	return nil
}
