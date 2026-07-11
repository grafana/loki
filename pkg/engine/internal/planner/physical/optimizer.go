package physical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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

// optimize runs the optimization to a fixed point and reports whether it changed
// the node at least once.
func (o *Optimization) optimize(node Node) bool {
	iterations, maxIterations := 0, 10
	applied := false

	for iterations < maxIterations {
		iterations++

		if !o.applyRules(node) {
			// Stop immediately if an optimization pass produced no changes.
			break
		}
		applied = true
	}

	return applied
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

// Optimize runs every optimization pass over node and returns a map from
// optimization name to whether it applied at least once.
func (o *Optimizer) Optimize(node Node) map[string]bool {
	firings := make(map[string]bool, len(o.optimisations))
	for _, optimisation := range o.optimisations {
		applied := optimisation.optimize(node)
		firings[optimisation.name] = applied
	}
	return firings
}

// addUniqueColumnExpr appends colExpr unless it duplicates an existing
// entry by (name, type), and additionally treats [ColumnTypeAmbiguous] as
// absorbing same-name Label / Metadata refs — the scan projects both
// Label (streams section) and Metadata (logs section) columns for an
// Ambiguous ref anyway, so retaining separate Label / Metadata entries
// would double-load the same storage column.
//
// Builtin is NOT absorbed by Ambiguous: `(X, Ambiguous)` and `(X, Builtin)`
// both remain. The executor's ambiguous lookup skips Builtin,
// so both refs are semantically distinct.
func addUniqueColumnExpr(projections []ColumnExpression, colExpr *ColumnExpr) ([]ColumnExpression, bool) {
	// If we're adding a Label/Metadata ref, drop it silently when a
	// same-name Ambiguous ref is already present — the Ambiguous entry
	// already causes the same storage column to be loaded.
	if colExpr.Ref.Type == types.ColumnTypeLabel || colExpr.Ref.Type == types.ColumnTypeMetadata {
		for _, existing := range projections {
			e, ok := existing.(*ColumnExpr)
			if !ok {
				continue
			}
			if e.Ref.Column == colExpr.Ref.Column && e.Ref.Type == types.ColumnTypeAmbiguous {
				return projections, false // absorbed
			}
		}
	}

	// Exact (name, type) dedup.
	for _, existing := range projections {
		e, ok := existing.(*ColumnExpr)
		if !ok {
			continue
		}
		if e.Ref.Column == colExpr.Ref.Column && e.Ref.Type == colExpr.Ref.Type {
			return projections, false
		}
	}

	// If we're adding an Ambiguous ref, remove any same-name Label / Metadata refs it absorbs.
	if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
		filtered := make([]ColumnExpression, 0, len(projections)+1)
		for _, existing := range projections {
			e, ok := existing.(*ColumnExpr)
			if !ok {
				filtered = append(filtered, existing)
				continue
			}
			if e.Ref.Column == colExpr.Ref.Column &&
				(e.Ref.Type == types.ColumnTypeLabel || e.Ref.Type == types.ColumnTypeMetadata) {
				continue // absorbed by the new Ambiguous
			}
			filtered = append(filtered, existing)
		}
		return append(filtered, colExpr), true
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
