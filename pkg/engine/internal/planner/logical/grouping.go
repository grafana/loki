package logical

// Grouping represents the grouping by/without label(s) for vector aggregators and range vector aggregators.
type Grouping struct {
	Columns []ColumnRef // The columns for grouping
	Without bool        // The grouping mode
}

var (
	NoGrouping = Grouping{Without: true}
)
