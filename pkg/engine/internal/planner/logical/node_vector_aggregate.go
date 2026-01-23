package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// VectorAggregation represents a logical plan node that performs vector aggregations.
// It computes aggregations over time series data at each timestamp instant
// grouping results by specified dimensions.
type VectorAggregation struct {
	b baseNode

	Table Value // The table relation to aggregate.

	Grouping Grouping // The grouping

	// The type of aggregation operation to perform (e.g., sum, min, max)
	Operation types.VectorAggregationType
}

var (
	_ Value       = (*VectorAggregation)(nil)
	_ Instruction = (*VectorAggregation)(nil)
)

// Name returns an identifier for the VectorAggregation operation.
func (v *VectorAggregation) Name() string { return v.b.Name() }

// String returns the disassembled SSA form of the VectorAggregation instruction.
func (v *VectorAggregation) String() string {
	props := fmt.Sprintf("operation=%s", v.Operation)

	grouping := ""
	if len(v.Grouping.Columns) > 0 {
		for i, columnRef := range v.Grouping.Columns {
			if i > 0 {
				grouping += ", "
			}
			grouping += columnRef.String()
		}
	}
	if v.Grouping.Without {
		if len(v.Grouping.Columns) > 0 {
			props = fmt.Sprintf("%s, group_without=(%s)", props, grouping)
		}
	} else {
		props = fmt.Sprintf("%s, group_by=(%s)", props, grouping)
	}

	return fmt.Sprintf("VECTOR_AGGREGATION %s [%s]", v.Table.Name(), props)
}

// Operands appends the operands of v to the provided slice. The pointers may
// be modified to change operands of v.
func (v *VectorAggregation) Operands(buf []*Value) []*Value {
	// NOTE(rfratto): Only fields of type Value are considered operands, so
	// v.Grouping.Columns is ignored here. Should that change?
	return append(buf, &v.Table)
}

// Referrers returns a list of instructions that reference the VectorAggregation.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (v *VectorAggregation) Referrers() *[]Instruction { return &v.b.referrers }

func (v *VectorAggregation) base() *baseNode { return &v.b }
func (v *VectorAggregation) isInstruction()  {}
func (v *VectorAggregation) isValue()        {}
