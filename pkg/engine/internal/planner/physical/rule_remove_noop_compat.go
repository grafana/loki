package physical

import "github.com/grafana/loki/v3/pkg/engine/internal/types"

var _ rule = (*removeNoopCompat)(nil)

// removeNoopCompat removes parsed compatibility after line_format projections.
// line_format does not produce parsed columns, so the compatibility node would
// only reprocess columns normalized by earlier parser stages.
type removeNoopCompat struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopCompat) apply(_ Node) bool {
	// Parallel pushdown may replace the root before cleanup runs, so inspect the
	// current graph instead of walking from the original root.
	var nodes []Node
	for node := range r.plan.graph.Nodes() {
		compat, ok := node.(*ColumnCompat)
		if !ok || compat.Source != types.ColumnTypeParsed {
			continue
		}

		children := r.plan.Children(compat)
		if len(children) != 1 {
			continue
		}

		projection, ok := children[0].(*Projection)
		if !ok || len(projection.Expressions) != 1 {
			continue
		}

		expr, ok := projection.Expressions[0].(*VariadicExpr)
		if ok && expr.Op == types.VariadicOpParseLinefmt {
			nodes = append(nodes, node)
		}
	}

	for _, node := range nodes {
		r.plan.graph.Eliminate(node)
	}
	return len(nodes) > 0
}
