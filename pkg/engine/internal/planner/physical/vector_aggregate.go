package physical

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// VectorAggregation represents a physical plan node that performs vector aggregations.
// It computes aggregations over time series data at each timestamp instant,
// grouping results by specified dimensions.
type VectorAggregation struct {
	NodeID ulid.ULID

	Grouping Grouping // Grouping of the data.

	// Operation defines the type of aggregation operation to perform (e.g., sum, min, max)
	Operation types.VectorAggregationType
}

// ID implements the [Node] interface.
// Returns the ULID that uniquely identifies the node in the plan.
func (v *VectorAggregation) ID() ulid.ULID { return v.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (v *VectorAggregation) Clone() Node {
	return &VectorAggregation{
		NodeID: ulid.Make(),

		Grouping: Grouping{
			Columns: cloneExpressions(v.Grouping.Columns),
			Without: v.Grouping.Without,
		},
		Operation: v.Operation,
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*VectorAggregation) Type() NodeType {
	return NodeTypeVectorAggregation
}
