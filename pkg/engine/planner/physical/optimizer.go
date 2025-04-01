package physical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// A rule is a tranformation that can be applied on a Node.
type rule interface {
	// apply tries to apply the transformation on the node.
	// It returns the node and a boolean indicating whether the transformation has been applied.
	apply(Node) (Node, bool)
}

// removeNoopPredicate is a rule that removes Filter nodes without predicates.
type removeNoopPredicate struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopPredicate) apply(node Node) (Node, bool) {
	changed := false
	switch node := node.(type) {
	case *Filter:
		if len(node.Predicates) == 0 {
			r.plan.removeNode(node)
			changed = true
		}
	}
	return node, changed
}

var _ rule = (*removeNoopPredicate)(nil)

// predicatePushdown is a rule that moves down filter predicates to the scan nodes.
type predicatePushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *predicatePushdown) apply(node Node) (Node, bool) {
	changed := false
	switch node := node.(type) {
	case *Filter:
		for i := 0; i < len(node.Predicates); i++ {
			if ok := r.applyPredicatePushdown(node, node.Predicates[i]); ok {
				changed = true
				// remove predicates that have been pushed down
				node.Predicates = append(node.Predicates[:i], node.Predicates[i+1:]...)
				i--
			}
		}
	}
	return node, changed
}

func (r *predicatePushdown) applyPredicatePushdown(node Node, predicate Expression) bool {
	switch node := node.(type) {
	case *DataObjScan:
		if canApplyPredicate(predicate) {
			node.Predicates = append(node.Predicates, predicate)
			return true
		}
		return false
	}
	for _, child := range r.plan.Children(node) {
		if ok := r.applyPredicatePushdown(child, predicate); !ok {
			return ok
		}
	}
	return true
}

func canApplyPredicate(predicate Expression) bool {
	switch pred := predicate.(type) {
	case *BinaryExpr:
		return canApplyPredicate(pred.Left) && canApplyPredicate(pred.Right)
	case *ColumnExpr:
		return pred.ColumnType == types.ColumnTypeBuiltin || pred.ColumnType == types.ColumnTypeMetadata
	case *LiteralExpr:
		return true
	default:
		return false
	}
}

var _ rule = (*predicatePushdown)(nil)

// limitPushdown is a rule that moves down the limit to the scan nodes.
type limitPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *limitPushdown) apply(node Node) (Node, bool) {
	switch node := node.(type) {
	case *Limit:
		ok := r.applyLimitPushdown(node, node.Limit)
		return node, ok
	}
	return node, false
}

func (r *limitPushdown) applyLimitPushdown(node Node, limit uint32) bool {
	switch node := node.(type) {
	case *DataObjScan:
		node.Limit = max(node.Limit, limit)
		return true
	}
	for _, child := range r.plan.Children(node) {
		if ok := r.applyLimitPushdown(child, limit); !ok {
			return ok
		}
	}
	return true
}

var _ rule = (*limitPushdown)(nil)

// optimization represents a single optimization pass and can hold multiple rules.
type optimization struct {
	plan  *Plan
	name  string
	rules []rule
}

func newOptimization(name string, plan *Plan) *optimization {
	return &optimization{
		name: name,
		plan: plan,
	}
}

func (o *optimization) withRules(rules ...rule) *optimization {
	o.rules = append(o.rules, rules...)
	return o
}

func (o *optimization) optimize(node Node) Node {
	changed := true // initialize with true, so it can be used as condition in the for-loop
	iterations := 0
	maxIterations := 3 // TODO(chaudum): Do we really need multiple optimization passes?

	for changed && iterations < maxIterations {
		changed = false //nolint:ineffassign
		iterations++

		node, changed = o.applyRules(node)
	}

	return node
}

func (o *optimization) applyRules(node Node) (Node, bool) {
	anyChanged := false

	for _, child := range o.plan.Children(node) {
		_, changed := o.applyRules(child)
		if changed {
			anyChanged = true
		}
	}

	for _, rule := range o.rules {
		_, changed := rule.apply(node)
		if changed {
			anyChanged = true
		}
	}

	return node, anyChanged
}

// The optimizer can optimize physical plans using the provided optimization passes.
type optimizer struct {
	plan   *Plan
	passes []*optimization
}

func newOptimizer(plan *Plan, passes []*optimization) *optimizer {
	return &optimizer{plan: plan, passes: passes}
}

func (o *optimizer) optimize(node Node) Node {
	for _, pass := range o.passes {
		node = pass.optimize(node)
	}
	return node
}
