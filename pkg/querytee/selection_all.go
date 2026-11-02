package querytee

import (
	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
)

const selectionStrategyAll = "all"

// allStrategy selects every request, whatever its type, tenants or time range. The secondary
// backend then gets the same traffic as the primary backend.
type allStrategy struct{}

// Select implements SelectionStrategy.
func (allStrategy) Select(_ []string, _ queryrangebase.Request) Selection {
	return Selection{Selected: true}
}
