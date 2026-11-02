package scheduler

import (
	"fmt"

	"github.com/grafana/loki/v4/pkg/logline"
)

// Config is what the scheduler reads from the logline.compaction and
// logline.retention sections.
type Config struct {
	Compaction logline.CompactionConfig
	Retention  logline.RetentionConfig
}

// Validate applies defaults for zero values and checks both sections.
func (c *Config) Validate() error {
	if err := c.Compaction.ValidateScheduler(); err != nil {
		return fmt.Errorf("invalid compaction config: %w", err)
	}
	if err := c.Retention.Validate(); err != nil {
		return fmt.Errorf("invalid retention config: %w", err)
	}
	return nil
}
