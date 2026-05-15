package physical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// A rule is a transformation that can be applied on a Node.
type rule interface {
	// apply tries to apply the transformation on the node.
	// It returns a boolean indicating whether the transformation has been applied.
	apply(Node) bool
}

func WorkflowOptimizations(plan *Plan) []*Optimization {
	return []*Optimization{
		newOptimization("ClampPredicates", plan).withRules(
			&clampPredicates{plan: plan}),
	}
}

// Optimization represents a single Optimization pass and can hold multiple rules.
type Optimization struct {
	plan  *Plan
	name  string
	rules []rule
}

func newOptimization(name string, plan *Plan) *Optimization {
	return &Optimization{
		name: name,
		plan: plan,
	}
}

func (o *Optimization) withRules(rules ...rule) *Optimization {
	o.rules = append(o.rules, rules...)
	return o
}

func (o *Optimization) optimize(node Node) {
	iterations, maxIterations := 0, 10

	for iterations < maxIterations {
		iterations++

		if !o.applyRules(node) {
			// Stop immediately if an optimization pass produced no changes.
			break
		}
	}
}

func (o *Optimization) applyRules(node Node) bool {
	anyChanged := false

	for _, rule := range o.rules {
		if rule.apply(node) {
			anyChanged = true
		}
	}

	return anyChanged
}

// The Optimizer can optimize physical plans using the provided optimization passes.
type Optimizer struct {
	plan          *Plan
	optimisations []*Optimization
}

func NewOptimizer(plan *Plan, passes []*Optimization) *Optimizer {
	return &Optimizer{plan: plan, optimisations: passes}
}

func (o *Optimizer) Optimize(node Node) {
	for _, optimisation := range o.optimisations {
		optimisation.optimize(node)
	}
}

// addUniqueColumnExpr adds a column to the projections list if it's not already present
func addUniqueColumnExpr(projections []ColumnExpression, colExpr *ColumnExpr) ([]ColumnExpression, bool) {
	for _, existing := range projections {
		if existingCol, ok := existing.(*ColumnExpr); ok {
			if existingCol.Ref.Column == colExpr.Ref.Column {
				return projections, false // already exists
			}
		}
	}
	return append(projections, colExpr), true
}

// findMatchingNodes finds all nodes in the plan tree that match the given matchFn.
func findMatchingNodes(plan *Plan, root Node, matchFn func(Node) bool) []Node {
	var result []Node
	// Using PostOrderWalk to return child nodes first.
	// This can be useful for optimizations like predicate pushdown
	// where it is ideal to process child Filter before parent Filter.
	_ = plan.graph.Walk(root, func(node Node) error {
		if matchFn(node) {
			result = append(result, node)
		}
		return nil
	}, dag.PostOrderWalk)
	return result
}
