package dataset

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Helper types.
type (
	// ColumnInfo describes a column.
	ColumnInfo struct {
		Name        string                    // Name of the column, if any.
		Type        datasetmd.ValueType       // Type of values in the column.
		Compression datasetmd.CompressionType // Compression used for the column.

		RowsCount        int // Total number of rows in the column.
		ValuesCount      int // Total number of non-NULL values in the column.
		CompressedSize   int // Total size of all pages in the column after compression.
		UncompressedSize int // Total size of all pages in the column before compression.

		Statistics *datasetmd.Statistics // Optional statistics for the column.
	}
)

// A Column represents a sequence of values within a dataset. Columns are split
// up across one or more [Page]s to limit the amount of memory needed to read a
// portion of the column at a time.
type Column interface {
	// ColumnInfo returns the metadata for the Column.
	ColumnInfo() *ColumnInfo

	// ListPages returns the set of ordered pages in the column.
	ListPages(ctx context.Context) result.Seq[Page]
}

// MemColumn holds a set of pages of a common type.
type MemColumn struct {
	Info  ColumnInfo // Information about the column.
	Pages []*MemPage // The set of pages in the column.
}

var _ Column = (*MemColumn)(nil)

// ColumnInfo implements [Column] and returns c.Info.
func (c *MemColumn) ColumnInfo() *ColumnInfo { return &c.Info }

// ListPages implements [Column] and iterates through c.Pages.
func (c *MemColumn) ListPages(_ context.Context) result.Seq[Page] {
	return result.Iter(func(yield func(Page) bool) error {
		for _, p := range c.Pages {
			if !yield(p) {
				return nil
			}
		}

		return nil
	})
}
