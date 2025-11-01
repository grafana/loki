package physical

import (
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// TODO: Rename based on the actual implementation.
type RangeAggregation struct {
	NodeID ulid.ULID

	PartitionBy []ColumnExpression // Columns to partition the data by.

	Operation types.RangeAggregationType
	Start     time.Time
	End       time.Time
	Step      time.Duration // optional for instant queries
	Range     time.Duration
}

// ID returns a string that uniquely identifies the node in the plan.
func (r *RangeAggregation) ID() string { return r.NodeID.String() }

// Clone returns a deep copy of the node (minus its ID).
func (r *RangeAggregation) Clone() Node {
	return &RangeAggregation{
		PartitionBy: cloneExpressions(r.PartitionBy),

		Operation: r.Operation,
		Start:     r.Start,
		End:       r.End,
		Step:      r.Step,
		Range:     r.Range,
	}
}

func (r *RangeAggregation) Type() NodeType {
	return NodeTypeRangeAggregation
}
