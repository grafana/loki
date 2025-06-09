package bench

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Store represents a storage backend for log data
type Store interface {
	// Write writes a batch of streams to the store
	Write(ctx context.Context, streams []logproto.Stream) error
	// Name returns the name of the store implementation
	Name() string
	// Close flushes any remaining data and closes resources
	Close() error
}

// Builder helps construct test datasets using multiple stores
type Builder struct {
	stores []Store
	gen    *Generator
	dir    string
}

// NewBuilder creates a new Builder
func NewBuilder(dir string, opt Opt, stores ...Store) *Builder {
	return &Builder{
		stores: stores,
		gen:    NewGenerator(opt),
		dir:    dir,
	}
}

// Generate generates and stores the specified amount of data across all stores
func (b *Builder) Generate(ctx context.Context, targetSize int64) error {
	fmt.Printf("Generating %s of data\n", formatBytes(targetSize))
	// Save the generator config once at the root directory
	if err := SaveConfig(b.dir, &b.gen.config); err != nil {
		return fmt.Errorf("failed to save generator config: %w", err)
	}

	var totalSize int64
	lastProgress := 0

	for batch := range b.gen.Batches() {
		// Write to all stores
		for _, store := range b.stores {
			if err := store.Write(ctx, batch.Streams); err != nil {
				return fmt.Errorf("failed to write to store %s: %w", store.Name(), err)
			}
		}
		totalSize += int64(batch.Size())
		// Report progress every 5%
		progress := int(float64(totalSize) / float64(targetSize) * 100)
		if progress/5 > lastProgress/5 {
			fmt.Printf("Generated %d%% (%s/%s)\n", progress, formatBytes(totalSize), formatBytes(targetSize))
			lastProgress = progress
		}

		if totalSize >= targetSize {
			break
		}
	}

	// Close all stores to ensure data is flushed
	for _, store := range b.stores {
		if err := store.Close(); err != nil {
			return fmt.Errorf("failed to close store %s: %w", store.Name(), err)
		}
	}

	fmt.Printf("Generated 100%% (%s/%s)\n", formatBytes(totalSize), formatBytes(targetSize))
	return nil
}

// formatBytes converts bytes to a human readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
