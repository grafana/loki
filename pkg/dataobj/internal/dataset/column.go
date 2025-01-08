package dataset

import "github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"

// Helper types.
type (
	// ColumnInfo describes a column.
	ColumnInfo struct {
		Name        string                    // Name of the column, if any.
		Type        datasetmd.ValueType       // Type of values in the column.
		Compression datasetmd.CompressionType // Compression used for the column.

		RowsCount        int // Total number of rows in the column.
		CompressedSize   int // Total size of all pages in the column after compression.
		UncompressedSize int // Total size of all pages in the column before compression.

		Statistics *datasetmd.Statistics // Optional statistics for the column.
	}
)

// MemColumn holds a set of pages of a common type.
type MemColumn struct {
	Info  ColumnInfo // Information about the column.
	Pages []*MemPage // The set of pages in the column.
}
