package physical

var _ rule = (*limitPushdown)(nil)

// limitPushdown is a rule that moves down the limit to the scan nodes.
type limitPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *limitPushdown) apply(root Node) bool {
	// collect limit nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Limit)
		return ok
	})

	// propagate limit to target child nodes.
	changed := false
	for _, n := range nodes {
		limit := n.(*Limit)
		if r.applyToTargets(limit, limit.Fetch) {
			changed = true
		}
	}
	return changed
}

// applyToTargets applies limit on target nodes.
func (r *limitPushdown) applyToTargets(node Node, limit uint32) bool {
	var changed bool
	switch node := node.(type) {
	case *TopK:
		node.K = max(node.K, int(limit))
		changed = true
	case *Filter:
		// If there is a filter, child nodes may need to read up to all their lines
		// to successfully apply the filter, so stop applying limit pushdown.
		return false
	}

	// Continue to children
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, limit) {
			changed = true
		}
	}
	return changed
}
