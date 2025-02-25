package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// DataFrame provides an ergonomic interface for building logical query plans
type DataFrame struct {
	plan Plan
}

// NewDataFrame creates a new DataFrame from a logical plan
func NewDataFrame(plan Plan) *DataFrame {
	return &DataFrame{plan: plan}
}

// Project applies a projection to the DataFrame
func (df *DataFrame) Project(exprs []Expr) *DataFrame {
	return &DataFrame{
		plan: NewProjection(df.plan, exprs),
	}
}

// Filter applies a filter to the DataFrame
func (df *DataFrame) Filter(expr Expr) *DataFrame {
	return &DataFrame{
		plan: NewFilter(df.plan, expr),
	}
}

// Aggregate applies grouping and aggregation to the DataFrame
func (df *DataFrame) Aggregate(groupBy []Expr, aggExprs []AggregateExpr) *DataFrame {
	return &DataFrame{
		plan: NewAggregate(df.plan, groupBy, aggExprs),
	}
}

// Schema returns the schema of the data that will be produced by this DataFrame
func (df *DataFrame) Schema() schema.Schema {
	return df.plan.Schema()
}

// LogicalPlan returns the underlying logical plan
func (df *DataFrame) LogicalPlan() Plan {
	return df.plan
}

// ToSSA converts the DataFrame to SSA form
func (df *DataFrame) ToSSA() (*SSAForm, error) {
	return ConvertToSSA(df.plan)
}
