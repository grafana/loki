package types

import (
	"fmt"
)

// ColumnType denotes the column type for a [ColumnRef].
type ColumnType int

// Recognized values of [ColumnType].
const (
	// ColumnTypeInvalid indicates an invalid column type.
	ColumnTypeInvalid ColumnType = iota

	ColumnTypeBuiltin   // ColumnTypeBuiltin represents a builtin column (such as timestamp).
	ColumnTypeLabel     // ColumnTypeLabel represents a column from a stream label.
	ColumnTypeMetadata  // ColumnTypeMetadata represents a column from a log metadata.
	ColumnTypeParsed    // ColumnTypeParsed represents a parsed column from a parser stage.
	ColumnTypeAmbiguous // ColumnTypeAmbiguous represents a column that can either be a builtin, label, metadata, or parsed.
	ColumnTypeGenerated // ColumnTypeGenerated represents a column that is generated from an expression or computation.
)

// Names of the builtin columns.
const (
	ColumnNameBuiltinTimestamp = "timestamp"
	ColumnNameBuiltinMessage   = "message"
	ColumnNameGeneratedValue   = "value"

	MetadataKeyColumnType     = "column_type"
	MetadataKeyColumnDataType = "column_datatype"
)

var ctNames = [7]string{"invalid", "builtin", "label", "metadata", "parsed", "ambiguous", "generated"}

// String returns a human-readable representation of the column type.
func (ct ColumnType) String() string {
	return ctNames[ct]
}

// ColumnTypeFromString returns the [ColumnType] from its string representation.
func ColumnTypeFromString(ct string) ColumnType {
	switch ct {
	case ctNames[1]:
		return ColumnTypeBuiltin
	case ctNames[2]:
		return ColumnTypeLabel
	case ctNames[3]:
		return ColumnTypeMetadata
	case ctNames[4]:
		return ColumnTypeParsed
	case ctNames[5]:
		return ColumnTypeAmbiguous
	default:
		panic(fmt.Sprintf("invalid column type: %s", ct))
	}
}

// A ColumnRef referenes a column within a table relation.
type ColumnRef struct {
	Column string     // Name of the column being referenced.
	Type   ColumnType // Type of the column being referenced.
}

// Name returns the identifier of the ColumnRef, which combines the column type
// and column name being referenced.
func (c *ColumnRef) String() string {
	return fmt.Sprintf("%s.%s", c.Type, c.Column)
}
