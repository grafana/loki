package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A ColumnRef referenes a column within a table relation. ColumnRef only
// implements [Value].
type ColumnRef struct {
	ref types.ColumnRef
}

var (
	_ Value = (*ColumnRef)(nil)
)

// Name returns the identifier of the ColumnRef, which combines the column type
// and column name being referenced.
func (c *ColumnRef) Name() string {
	return c.ref.String()
}

// String returns [ColumnRef.Name].
func (c *ColumnRef) String() string {
	return c.ref.String()
}

// Ref returns the wrapped [types.ColumnRef].
func (c *ColumnRef) Ref() types.ColumnRef {
	return c.ref
}

// Schema returns the schema of the column being referenced.
func (c *ColumnRef) Schema() *schema.Schema {
	// TODO(rfratto): Update *schema.Schema to allow representing a single
	// column.
	return nil
}

func (c *ColumnRef) isValue() {}

func NewColumnRef(name string, ty types.ColumnType) *ColumnRef {
	return &ColumnRef{
		ref: types.ColumnRef{
			Column: name,
			Type:   ty,
		},
	}
}
