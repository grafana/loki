package types

import "fmt"

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
)

// Names of the builtin columns.
const (
	ColumnNameBuiltinTimestamp = "timestamp"
	ColumnNameBuiltinLine      = "line"
	ColumnTypeMetadataKey      = "column_type"
)

// String returns a human-readable representation of the column type.
func (ct ColumnType) String() string {
	switch ct {
	case ColumnTypeInvalid:
		return typeInvalid
	case ColumnTypeBuiltin:
		return "builtin"
	case ColumnTypeLabel:
		return "label"
	case ColumnTypeMetadata:
		return "metadata"
	case ColumnTypeParsed:
		return "parsed"
	case ColumnTypeAmbiguous:
		return "ambiguous"
	default:
		return fmt.Sprintf("ColumnType(%d)", ct)
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
