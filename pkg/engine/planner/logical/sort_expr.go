package logical

// SortExpr represents a sort expression in a query plan.
// It encapsulates an expression to sort on, a sort direction (ascending or descending),
// and how NULL values should be handled (first or last).
type SortExpr struct {
	// name is a descriptive name for the sort expression
	name string
	// expr is the expression to sort on
	expr Expr
	// asc indicates whether to sort in ascending order (true) or descending order (false)
	asc bool
	// nullsFirst indicates whether NULL values should appear first (true) or last (false)
	nullsFirst bool
}

// NewSortExpr creates a new sort expression.
//
// Parameters:
//   - name: A descriptive name for the sort expression
//   - expr: The expression to sort on
//   - asc: Whether to sort in ascending order (true) or descending order (false)
//   - nullsFirst: Whether NULL values should appear first (true) or last (false)
//
// Example usage:
//
//	// Sort by age in ascending order, NULLs last
//	sortExpr := NewSortExpr("sort_by_age", Col("age"), true, false)
//
//	// Sort by name in descending order, NULLs first
//	sortExpr := NewSortExpr("sort_by_name", Col("name"), false, true)
func NewSortExpr(name string, expr Expr, asc bool, nullsFirst bool) SortExpr {
	return SortExpr{
		name:       name,
		expr:       expr,
		asc:        asc,
		nullsFirst: nullsFirst,
	}
}

// Name returns the name of the sort expression.
func (s SortExpr) Name() string {
	return s.name
}

// Expr returns the expression to sort on.
func (s SortExpr) Expr() Expr {
	return s.expr
}

// Asc returns whether the sort is in ascending order.
func (s SortExpr) Asc() bool {
	return s.asc
}

// NullsFirst returns whether NULL values should appear first.
func (s SortExpr) NullsFirst() bool {
	return s.nullsFirst
}
