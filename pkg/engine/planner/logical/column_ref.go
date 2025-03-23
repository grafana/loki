package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

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
		return "invalid"
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

// A ColumnRef referenes a column within a table relation. ColumnRef only
// implements [Value].
type ColumnRef struct {
	Column string     // Name of the column being referenced.
	Type   ColumnType // Type of the column being referenced.
}

var (
	_ Value = (*ColumnRef)(nil)
)

// Name returns the identifier of the ColumnRef, which combines the column type
// and column name being referenced.
func (c *ColumnRef) Name() string {
	return fmt.Sprintf("%s.%s", c.Type, c.Column)
}

// String returns [ColumnRef.Name].
func (c *ColumnRef) String() string { return c.Name() }

// Schema returns the schema of the column being referenced.
func (c *ColumnRef) Schema() *schema.Schema {
	// TODO(rfratto): Update *schema.Schema to allow representing a single
	// column.
	return nil
}

func (c *ColumnRef) isValue() {}
