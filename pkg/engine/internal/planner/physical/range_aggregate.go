package physical

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// TODO: Rename based on the actual implementation.
type RangeAggregation struct {
	id string

	PartitionBy []ColumnExpression // Columns to partition the data by.

	Operation types.RangeAggregationType
	Start     time.Time
	End       time.Time
	Step      time.Duration // optional for instant queries
	Range     time.Duration
}

func (r *RangeAggregation) ID() string {
	if r.id == "" {
		return fmt.Sprintf("%p", r)
	}

	return r.id
}

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

func (r *RangeAggregation) Accept(v Visitor) error {
	return v.VisitRangeAggregation(r)
}
