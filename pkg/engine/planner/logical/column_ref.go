package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A ColumnRef referenes a column within a table relation. ColumnRef only
// implements [Value].
type ColumnRef struct {
	Ref types.ColumnRef
}

var (
	_ Value = (*ColumnRef)(nil)
)

// Name returns the identifier of the ColumnRef, which combines the column type
// and column name being referenced.
func (c *ColumnRef) Name() string {
	return c.Ref.String()
}

// String returns [ColumnRef.Name].
func (c *ColumnRef) String() string {
	return c.Ref.String()
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
		Ref: types.ColumnRef{
			Column: name,
			Type:   ty,
		},
	}
}
