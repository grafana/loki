package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// VectorAggregation represents a logical plan node that performs vector aggregations.
// It computes aggregations over time series data at each timestamp instant
// grouping results by specified dimensions.
type VectorAggregation struct {
	id string

	Table Value // The table relation to aggregate.

	// The columns to group by. If empty, all rows are aggregated into a single result.
	GroupBy []ColumnRef

	// The type of aggregation operation to perform (e.g., sum, min, max)
	Operation types.VectorAggregationType
}

var (
	_ Value       = (*VectorAggregation)(nil)
	_ Instruction = (*VectorAggregation)(nil)
)

// Name returns an identifier for the VectorAggregation operation.
func (v *VectorAggregation) Name() string {
	if v.id != "" {
		return v.id
	}
	return fmt.Sprintf("%p", v)
}

// String returns the disassembled SSA form of the VectorAggregation instruction.
func (v *VectorAggregation) String() string {
	props := fmt.Sprintf("operation=%s", v.Operation)

	if len(v.GroupBy) > 0 {
		groupBy := ""
		for i, columnRef := range v.GroupBy {
			if i > 0 {
				groupBy += ", "
			}
			groupBy += columnRef.String()
		}
		props += fmt.Sprintf(", group_by=(%s)", groupBy)
	}

	return fmt.Sprintf("VECTOR_AGGREGATION %s [%s]", v.Table.Name(), props)
}

func (v *VectorAggregation) isInstruction() {}
func (v *VectorAggregation) isValue()       {}
