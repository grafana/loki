package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// VectorAggregation represents a physical plan node that performs vector aggregations.
// It computes aggregations over time series data at each timestamp instant,
// grouping results by specified dimensions.
type VectorAggregation struct {
	id string

	// GroupBy defines the columns to group by. If empty, all rows are aggregated into a single result.
	GroupBy []ColumnExpression

	// Operation defines the type of aggregation operation to perform (e.g., sum, min, max)
	Operation types.VectorAggregationType
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (v *VectorAggregation) ID() string {
	if v.id == "" {
		return fmt.Sprintf("%p", v)
	}
	return v.id
}

// Clone returns a deep copy of the node (minus its ID).
func (v *VectorAggregation) Clone() Node {
	return &VectorAggregation{
		GroupBy:   cloneExpressions(v.GroupBy),
		Operation: v.Operation,
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*VectorAggregation) Type() NodeType {
	return NodeTypeVectorAggregation
}
