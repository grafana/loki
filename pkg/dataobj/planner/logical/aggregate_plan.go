package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
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
func newAggregate(input Plan, groupExprs []Expr, aggExprs []AggregateExpr) *Aggregate {
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

// Type implements the ast interface
func (a *Aggregate) Type() PlanType {
	return PlanTypeAggregate
}

// GroupExprs implements the aggregateNode interface
func (a *Aggregate) GroupExprs() []Expr {
	return a.groupExprs
}

// AggregateExprs implements the aggregateNode interface
func (a *Aggregate) AggregateExprs() []AggregateExpr {
	return a.aggExprs
}

// Child implements the aggregateNode interface
func (a *Aggregate) Child() Plan {
	return a.input
}
