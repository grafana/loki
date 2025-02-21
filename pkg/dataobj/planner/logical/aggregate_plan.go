package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Aggregate implements Plan
var (
	_ Plan = &Aggregate{}
)

// Aggregate represents a plan node that performs aggregation operations.
// The output schema is organized with grouping columns followed by aggregate expressions.
// It often needs to be wrapped in a projection to achieve the desired column ordering.
type Aggregate struct {
	// input is the child plan node providing data to aggregate
	input Plan
	// groupExprs are the expressions to group by
	groupExprs []Expr
	// aggExprs are the aggregate expressions to compute
	aggExprs []AggregateExpr
}

// NewAggregate creates a new Aggregate plan node.
// The Aggregate logical plan calculates aggregates of underlying data such as
// calculating minimum, maximum, averages, and sums of data. Aggregates are often
// grouped by other columns (or expressions).
// A simple example would be SELECT region, SUM(sales) FROM orders GROUP BY region.
func NewAggregate(input Plan, groupExprs []Expr, aggExprs []AggregateExpr) *Aggregate {
	return &Aggregate{
		input:      input,
		groupExprs: groupExprs,
		aggExprs:   aggExprs,
	}
}

// Schema returns the schema of the data produced by this aggregate.
// The schema consists of group-by expressions followed by aggregate expressions.
func (a *Aggregate) Schema() schema.Schema {
	var columns []schema.ColumnSchema

	// Group expressions come first
	for _, expr := range a.groupExprs {
		columns = append(columns, expr.ToField(a.input))
	}

	// Followed by aggregate expressions
	for _, expr := range a.aggExprs {
		columns = append(columns, expr.ToField(a.input))
	}

	return schema.FromColumns(columns)
}

// Children returns the child plan nodes
func (a *Aggregate) Children() []Plan {
	return []Plan{a.input}
}

// Format implements format.Format
func (a *Aggregate) Format(fm format.Formatter) {
	// Collect grouping names
	var groupNames []string
	for _, expr := range a.groupExprs {
		groupNames = append(groupNames, expr.ToField(a.input).Name)
	}

	// Collect aggregate names
	var aggNames []string
	for _, expr := range a.aggExprs {
		aggNames = append(aggNames, expr.ToField(a.input).Name)
	}

	n := format.Node{
		Singletons: []string{"Aggregate"},
		Tuples: []format.ContentTuple{
			{
				Key:   "groupings",
				Value: format.ListContentFrom(groupNames...),
			},
			{
				Key:   "aggregates",
				Value: format.ListContentFrom(aggNames...),
			},
		},
	}

	nextFM := fm.WriteNode(n)

	// Format grouping expressions
	groupNode := format.Node{Singletons: []string{"GroupExprs"}}
	groupFM := nextFM.WriteNode(groupNode)
	for _, expr := range a.groupExprs {
		expr.Format(groupFM)
	}

	// Format aggregate expressions
	aggNode := format.Node{Singletons: []string{"AggregateExprs"}}
	aggFM := nextFM.WriteNode(aggNode)
	for _, expr := range a.aggExprs {
		expr.Format(aggFM)
	}

	// Format input plan
	a.input.Format(nextFM)
}
