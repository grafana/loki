package physical

import "github.com/grafana/loki/v3/pkg/engine/internal/types"

// Grouping represents the grouping by/without label(s) for vector aggregators and range vector aggregators.
type Grouping struct {
	Columns []ColumnExpression // The columns for grouping
	Mode    types.GroupingMode // The grouping mode
}
