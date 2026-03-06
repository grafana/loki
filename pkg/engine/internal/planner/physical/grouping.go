package physical

// Grouping represents the grouping by/without label(s) for vector aggregators and range vector aggregators.
type Grouping struct {
	Columns []ColumnExpression // The columns for grouping
	Without bool               // The grouping mode
}
