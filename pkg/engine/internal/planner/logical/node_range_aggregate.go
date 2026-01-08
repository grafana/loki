package logical

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
)

// RangeAggregation represents a logical plan node that performs aggregations over a time window.
// It is similar to window functions in SQL with a few important distinctions:
// 1. It evaluates the aggregation at step intervals unlike traditional window functions which are evaluated for each row.
// 2. It uses a time window defined by query [$range].
type RangeAggregation struct {
	b baseNode

	Table    Value    // The table relation to aggregate.
	Grouping Grouping // The grouping

	Operation     types.RangeAggregationType // The type of aggregation operation to perform.
	Start         time.Time
	End           time.Time
	Step          time.Duration
	RangeInterval time.Duration
}

var (
	_ Value       = (*RangeAggregation)(nil)
	_ Instruction = (*RangeAggregation)(nil)
)

// Name returns an identifier for the RangeAggregation operation.
func (r *RangeAggregation) Name() string { return r.b.Name() }

// String returns the disassembled SSA form of the RangeAggregation instruction.
func (r *RangeAggregation) String() string {
	props := fmt.Sprintf("operation=%s, start_ts=%s, end_ts=%s, step=%s, range=%s", r.Operation, util.FormatTimeRFC3339Nano(r.Start), util.FormatTimeRFC3339Nano(r.End), r.Step, r.RangeInterval)

	grouping := ""
	if len(r.Grouping.Columns) > 0 {
		for i, columnRef := range r.Grouping.Columns {
			if i > 0 {
				grouping += ", "
			}
			grouping += columnRef.String()
		}
	}
	if r.Grouping.Without {
		if len(r.Grouping.Columns) > 0 {
			props = fmt.Sprintf("group_without=(%s), %s", grouping, props)
		}
	} else {
		props = fmt.Sprintf("group_by=(%s), %s", grouping, props)
	}

	return fmt.Sprintf("RANGE_AGGREGATION %s [%s]", r.Table.Name(), props)
}

// Operands appends the operands of r to the provided slice. The pointers may
// be modified to change operands of r.
func (r *RangeAggregation) Operands(buf []*Value) []*Value {
	// NOTE(rfratto): Only fields of type Value are considered operands, so
	// r.Grouping.Columns is ignored here. Should that change?
	return append(buf, &r.Table)
}

// Referrers returns a list of instructions that reference the RangeAggregation.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (r *RangeAggregation) Referrers() *[]Instruction { return &r.b.referrers }

func (r *RangeAggregation) base() *baseNode { return &r.b }
func (r *RangeAggregation) isInstruction()  {}
func (r *RangeAggregation) isValue()        {}
