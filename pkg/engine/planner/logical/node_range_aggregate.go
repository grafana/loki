package logical

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// RangeAggregation represents a logical plan node that performs aggregations over a time window.
// It is similar to window functions in SQL with a few important distinctions:
// 1. It evaluates the aggregation at step intervals unlike traditional window functions which are evaluated for each row.
// 2. It uses a time window defined by query [$range].
// 3. It partitions by query-time streams if no partition by is specified.
type RangeAggregation struct {
	id string

	Table       Value       // The table relation to aggregate.
	PartitionBy []ColumnRef // The columns to partition by.

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
func (r *RangeAggregation) Name() string {
	if r.id != "" {
		return r.id
	}
	return fmt.Sprintf("%p", r)
}

// String returns the disassembled SSA form of the RangeAggregation instruction.
func (r *RangeAggregation) String() string {
	props := fmt.Sprintf("operation=%s, start_ts=%s, end_ts=%s, step=%s, range=%s", r.Operation, util.FormatTimeRFC3339Nano(r.Start), util.FormatTimeRFC3339Nano(r.End), r.Step, r.RangeInterval)

	if len(r.PartitionBy) > 0 {
		partitionBy := ""
		for i, columnRef := range r.PartitionBy {
			if i > 0 {
				partitionBy += ", "
			}
			partitionBy += columnRef.String()
		}

		return fmt.Sprintf("RANGE_AGGREGATION %s [partition_by=(%s), %s]", r.Table.Name(), partitionBy, props)
	}

	return fmt.Sprintf("RANGE_AGGREGATION %s [%s]", r.Table.Name(), props)
}

// Schema returns the schema of the sort plan.
func (r *RangeAggregation) Schema() *schema.Schema {
	// When partition by is specified, Schema is comprised of partition columns, timestamp and the aggregated value.
	if len(r.PartitionBy) > 0 {
		outputSchema := schema.Schema{
			Columns: make([]schema.ColumnSchema, 0, len(r.PartitionBy)+2),
		}

		for _, columnRef := range r.PartitionBy {
			outputSchema.Columns = append(outputSchema.Columns,
				schema.ColumnSchema{Name: columnRef.Ref.Column, Type: schema.ValueTypeString},
			)
		}

		outputSchema.Columns = append(outputSchema.Columns, schema.ColumnSchema{
			Name: types.ColumnNameBuiltinTimestamp,
			Type: schema.ValueTypeTimestamp,
		})
		// Using int64 since only count_over_time is supported.
		outputSchema.Columns = append(outputSchema.Columns, schema.ColumnSchema{
			Name: types.ColumnNameGeneratedValue,
			Type: schema.ValueTypeInt64,
		})
	}

	// If partition by is empty, we aggregate by query-time series.
	// Schema is then comprised of:
	// - input columns excluding message Column
	// - aggregated value column
	outputSchema := schema.Schema{
		Columns: make([]schema.ColumnSchema, 0, len(r.Table.Schema().Columns)),
	}
	for _, col := range r.Table.Schema().Columns {
		if col.Name != types.ColumnNameBuiltinMessage { // Exclude message column
			outputSchema.Columns = append(outputSchema.Columns, col)
		}
	}
	outputSchema.Columns = append(outputSchema.Columns, schema.ColumnSchema{
		Name: types.ColumnNameGeneratedValue,
		Type: schema.ValueTypeInt64,
	})
	return &outputSchema
}

func (r *RangeAggregation) isInstruction() {}
func (r *RangeAggregation) isValue()       {}
