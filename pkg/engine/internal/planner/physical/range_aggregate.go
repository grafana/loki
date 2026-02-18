package physical

import (
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// TODO: Rename based on the actual implementation.
type RangeAggregation struct {
	NodeID ulid.ULID

	Grouping       Grouping
	Operation      types.RangeAggregationType
	Start          time.Time
	End            time.Time
	Step           time.Duration // optional for instant queries
	Range          time.Duration
	MaxQuerySeries int // maximum number of unique series allowed (0 means no limit)

	// Columnar indicates that this node should use the columnar (batch)
	// execution path. Set by the ColumnarPromotion optimizer when grouping
	// labels are known (by-clause, not without) and windows are aligned
	// (Step == Range).
	Columnar bool
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (r *RangeAggregation) ID() ulid.ULID { return r.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (r *RangeAggregation) Clone() Node {
	return &RangeAggregation{
		NodeID: ulid.Make(),

		Grouping: Grouping{
			Columns: cloneExpressions(r.Grouping.Columns),
			Without: r.Grouping.Without,
		},
		Operation:      r.Operation,
		Start:          r.Start,
		End:            r.End,
		Step:           r.Step,
		Range:          r.Range,
		MaxQuerySeries: r.MaxQuerySeries,
		Columnar:       r.Columnar,
	}
}

func (r *RangeAggregation) Type() NodeType {
	return NodeTypeRangeAggregation
}
