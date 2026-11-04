package logline

import (
	"flag"
	"fmt"
	"time"
)

const (
	DefaultCompactedSourceDeleteDelay = time.Hour
	DefaultDeleteConcurrency          = 20
)

// RetentionConfig is the logline.retention section. It covers when indexes
// are deleted from object storage. Indexes are deleted in two cases: their
// data is older than the retention period, or a merged index replaced them.
//
// The retention period itself is not part of this section. It is the
// longest limits_config.retention_period across tenants, because one index
// holds the data of every tenant.
type RetentionConfig struct {
	// CompactedSourceDeleteDelay is how long a source index is kept after a
	// merged index replaced it. Readers holding an older snapshot can still
	// open the source during this window.
	CompactedSourceDeleteDelay time.Duration `yaml:"compacted_source_delete_delay"`
	// DeleteConcurrency is how many indexes are deleted in parallel.
	DeleteConcurrency int `yaml:"delete_concurrency"`
}

func (c *RetentionConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&c.CompactedSourceDeleteDelay, prefix+".compacted-source-delete-delay", DefaultCompactedSourceDeleteDelay,
		"How long an index is kept after compaction merged it into a larger index. Readers holding an older view of the store can still open it during this window.")
	f.IntVar(&c.DeleteConcurrency, prefix+".delete-concurrency", DefaultDeleteConcurrency,
		"Number of indexes deleted in parallel.")
}

// Validate applies defaults for zero values and checks the section. It
// mutates the receiver, so it must run before the config is used.
func (c *RetentionConfig) Validate() error {
	if c.CompactedSourceDeleteDelay == 0 {
		c.CompactedSourceDeleteDelay = DefaultCompactedSourceDeleteDelay
	}
	if c.DeleteConcurrency == 0 {
		c.DeleteConcurrency = DefaultDeleteConcurrency
	}

	if c.CompactedSourceDeleteDelay < 0 {
		return fmt.Errorf("compacted_source_delete_delay must be positive, got %v", c.CompactedSourceDeleteDelay)
	}
	if c.DeleteConcurrency < 0 {
		return fmt.Errorf("delete_concurrency must be positive, got %d", c.DeleteConcurrency)
	}
	return nil
}
