package testutil

import (
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

// MustPlan creates a QueryPlan from a selector string.
// Panics if the selector cannot be parsed.
func MustPlan(selector string) *plan.QueryPlan {
	return &plan.QueryPlan{
		AST: syntax.MustParseExpr(selector),
	}
}
