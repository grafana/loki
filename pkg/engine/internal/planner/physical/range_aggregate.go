package physical

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
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
	}
}

func (r *RangeAggregation) Type() NodeType {
	return NodeTypeRangeAggregation
}

func (r *RangeAggregation) CacheableKey() string {
	groupCols := expressionStrings(exprSliceToExpression(r.Grouping.Columns))
	return fmt.Sprintf("RangeAgg op=%s grouping(without=%v cols=%s) start=%s end=%s step=%s range=%s max_series=%d",
		r.Operation,
		r.Grouping.Without, cacheKeyListJoin(groupCols),
		util.FormatTimeRFC3339Nano(r.Start), util.FormatTimeRFC3339Nano(r.End),
		r.Step, r.Range, r.MaxQuerySeries)
}
