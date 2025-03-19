package logical

// SortExpr represents a sort expression in a query plan.
// It encapsulates an expression to sort on, a sort direction (ascending or descending),
// and how NULL values should be handled (first or last).
type SortExpr struct {
	Name       string // Identifier for the expression.
	Expr       Expr   // Expression to sort on.
	Ascending  bool   // If true, sorts in ascending order.
	NullsFirst bool   // Controls whether NULLs appear first (true) or last (false).
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
		Name:       name,
		Expr:       expr,
		Ascending:  asc,
		NullsFirst: nullsFirst,
	}
}
