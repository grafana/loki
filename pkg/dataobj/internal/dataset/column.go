package dataset

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Helper types.
type (
	// ColumnType represents the type of data stored in a column. Column types
	// are represented as a tuple of a physical and logical type.
	ColumnType struct {
		// Physical is the type of values physically stored within the column.
		Physical datasetmd.PhysicalType

		// Logical is a custom string indicating how dataset-derived sections
		// should interpret the physical type.
		Logical string
	}

	// ColumnDesc describes a column.
	ColumnDesc struct {
		Type        ColumnType                // Type of values in the column.
		Tag         string                    // Optional string to distinguish columns with the same type.
		Compression datasetmd.CompressionType // Compression used for the column.

		PagesCount       int // Total number of pages in the column.
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
	// ColumnDesc returns the metadata for the Column.
	ColumnDesc() *ColumnDesc

	// ListPages returns the set of ordered pages in the column.
	ListPages(ctx context.Context) result.Seq[Page]
}

// MemColumn holds a set of pages of a common type.
type MemColumn struct {
	Desc  ColumnDesc // Description of the column.
	Pages []*MemPage // The set of pages in the column.
}

var _ Column = (*MemColumn)(nil)

// ColumnDesc implements [Column] and returns c.Desc.
func (c *MemColumn) ColumnDesc() *ColumnDesc { return &c.Desc }

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
