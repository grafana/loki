package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// DataFrame provides an ergonomic interface for building logical query plans.
// It wraps a logical Plan and provides fluent methods for common operations
// like projection, filtering, and aggregation. This makes it easier to build
// complex query plans in a readable and maintainable way.
type DataFrame struct {
	plan Plan
}

// NewDataFrame creates a new DataFrame from a logical plan.
// This is typically used to wrap a table scan plan as the starting point
// for building a more complex query.
func NewDataFrame(plan Plan) *DataFrame {
	return &DataFrame{plan: plan}
}

// Project applies a projection to the DataFrame.
// It creates a new DataFrame with a projection plan that selects or computes
// the specified expressions from the input DataFrame.
// This corresponds to the SELECT clause in SQL.
func (df *DataFrame) Project(exprs []Expr) *DataFrame {
	return &DataFrame{
		plan: NewProjection(df.plan, exprs),
	}
}

// Filter applies a filter to the DataFrame.
// It creates a new DataFrame with a filter plan that selects rows from the
// input DataFrame based on the specified boolean expression.
// This corresponds to the WHERE clause in SQL.
func (df *DataFrame) Filter(expr Expr) *DataFrame {
	return &DataFrame{
		plan: NewFilter(df.plan, expr),
	}
}

// Aggregate applies grouping and aggregation to the DataFrame.
// It creates a new DataFrame with an aggregate plan that groups rows by the
// specified expressions and computes the specified aggregate expressions.
// This corresponds to the GROUP BY clause in SQL.
func (df *DataFrame) Aggregate(groupBy []Expr, aggExprs []AggregateExpr) *DataFrame {
	return &DataFrame{
		plan: NewAggregate(df.plan, groupBy, aggExprs),
	}
}

// Limit applies a row limit to the DataFrame.
// It creates a new DataFrame with a limit plan that restricts the number of rows
// returned, optionally with an offset to skip initial rows.
// This corresponds to the LIMIT and OFFSET clauses in SQL.
//
// Parameters:
//   - skip: Number of rows to skip before returning results (OFFSET in SQL).
//     Use 0 to start from the first row.
//   - fetch: Maximum number of rows to return after skipping (LIMIT in SQL).
//     Use 0 to return all remaining rows.
//
// Example usage:
//
//	// Return the first 10 rows
//	df = df.Limit(0, 10)
//
//	// Skip the first 20 rows and return the next 10
//	df = df.Limit(20, 10)
//
//	// Skip the first 100 rows and return all remaining rows
//	df = df.Limit(100, 0)
//
// The Limit operation is typically applied as the final step in a query,
// after filtering, projection, and aggregation.
func (df *DataFrame) Limit(skip uint64, fetch uint64) *DataFrame {
	return &DataFrame{
		plan: NewLimit(df.plan, skip, fetch),
	}
}

// Schema returns the schema of the data that will be produced by this DataFrame.
// This is useful for understanding the structure of the data that will result
// from executing the query plan.
func (df *DataFrame) Schema() schema.Schema {
	return df.plan.Schema()
}

// LogicalPlan returns the underlying logical plan.
// This is useful when you need to access the plan directly, such as when
// passing it to a function that operates on plans rather than DataFrames.
func (df *DataFrame) LogicalPlan() Plan {
	return df.plan
}

// ToSSA converts the DataFrame to SSA form.
// This is useful for optimizing and executing the query plan, as the SSA form
// is easier to analyze and transform than the tree-based logical plan.
func (df *DataFrame) ToSSA() (*SSAForm, error) {
	return ConvertToSSA(df.plan)
}

// Sort applies a sort operation to the DataFrame.
// It creates a new DataFrame with a sort plan that orders rows from the
// input DataFrame based on the specified sort expression.
// This corresponds to the ORDER BY clause in SQL.
//
// Parameters:
//   - expr: The sort expression specifying the column to sort by, sort direction,
//     and NULL handling.
//
// Example usage:
//
//	// Sort by age in ascending order, NULLs last
//	df = df.Sort(NewSortExpr("sort_by_age", Col("age"), true, false))
//
//	// Sort by name in descending order, NULLs first
//	df = df.Sort(NewSortExpr("sort_by_name", Col("name"), false, true))
//
// The Sort operation is typically applied after filtering and projection,
// but before limiting the results.
func (df *DataFrame) Sort(expr SortExpr) *DataFrame {
	return &DataFrame{
		plan: NewSort(df.plan, expr),
	}
}
