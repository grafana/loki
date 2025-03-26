package types

import "fmt"

// ColumnType denotes the column type for a [ColumnRef].
type ColumnType int

// Recognized values of [ColumnType].
const (
	// ColumnTypeInvalid indicates an invalid column type.
	ColumnTypeInvalid ColumnType = iota

	ColumnTypeBuiltin  // ColumnTypeBuiltin represents a builtin column (such as timestamp).
	ColumnTypeLabel    // ColumnTypeLabel represents a column from a stream label.
	ColumnTypeMetadata // ColumnTypeMetadata represents a column from a log metadata.
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
	default:
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
}
