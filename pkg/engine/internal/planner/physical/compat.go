package physical

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ColumnCompat represents a compactibilty operation in the physical plan that
// moves a values from a conflicting metadata column with a label column into a new column suffixed with `_extracted`.
type ColumnCompat struct {
	NodeID ulid.ULID

	// TODO(chaudum): These fields are poorly named. Come up with more descriptive names.
	Source      types.ColumnType // column type of the column that may colide with columns of the same name but with collision type
	Destination types.ColumnType // column type of the generated _extracted column (should be same as source)
	Collision   types.ColumnType // column type of the column that a source type column may collide with
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (m *ColumnCompat) ID() string { return m.NodeID.String() }

	return m.id
}

// Clone returns a deep copy of the node (minus its ID).
func (m *ColumnCompat) Clone() Node {
	return &ColumnCompat{
		Source:      m.Source,
		Destination: m.Destination,
		Collision:   m.Collision,
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (m *ColumnCompat) Type() NodeType {
	return NodeTypeCompat
}
