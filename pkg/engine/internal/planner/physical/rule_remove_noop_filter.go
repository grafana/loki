package physical

var _ rule = (*removeNoopFilter)(nil)

// removeNoopFilter is a rule that removes Filter nodes without predicates.
type removeNoopFilter struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopFilter) apply(root Node) bool {
	// collect filter nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Filter)
		return ok
	})

	changed := false
	for _, n := range nodes {
		filter := n.(*Filter)
		if len(filter.Predicates) == 0 {
			r.plan.graph.Eliminate(filter)
			changed = true
		}
	}

	return changed
}
