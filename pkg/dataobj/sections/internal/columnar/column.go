package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd/v2"
)

// PhysicalType represents the physical type of data encoded within a column.
type PhysicalType int

const (
	PhysicalTypeUnspecified PhysicalType = iota
	PhysicalTypeInt64
	PhysicalTypeUint64
	PhysicalTypeBinary
)

var physicalTypeNames = map[PhysicalType]string{
	PhysicalTypeUnspecified: "Unspecified",
	PhysicalTypeInt64:       "Int64",
	PhysicalTypeUint64:      "Uint64",
	PhysicalTypeBinary:      "Binary",
}

// ConvertPhysicalType returns a [PhysicalType] from the protobuf type.
func ConvertPhysicalType(ty datasetmd.PhysicalType) PhysicalType {
	switch ty {
	case datasetmd.PHYSICAL_TYPE_INT64:
		return PhysicalTypeInt64
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return PhysicalTypeUint64
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return PhysicalTypeBinary
	}

	return PhysicalTypeUnspecified
}

// Proto returns the protobuf type of pt.
func (pt PhysicalType) Proto() datasetmd.PhysicalType {
	switch pt {
	case PhysicalTypeInt64:
		return datasetmd.PHYSICAL_TYPE_INT64
	case PhysicalTypeUint64:
		return datasetmd.PHYSICAL_TYPE_UINT64
	case PhysicalTypeBinary:
		return datasetmd.PHYSICAL_TYPE_BINARY
	}

	return datasetmd.PHYSICAL_TYPE_UNSPECIFIED
}

// String returns the human-readable name of pt.
func (pt PhysicalType) String() string {
	text, ok := physicalTypeNames[pt]
	if !ok {
		return fmt.Sprintf("PhysicalType(%d)", pt)
	}
	return text
}

// Helper types.
type (
	// ColumnType represents the type of data stored in a column. Column types
	// are represented as a tuple of a physical and logical type.
	ColumnType struct {
		// Physical is the type of values physically stored within the column.
		Physical PhysicalType

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
