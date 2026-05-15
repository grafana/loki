package physical

import (
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var _ rule = (*predicatePushdown)(nil)

// predicatePushdown is a rule that moves down filter predicates to the scan nodes.
type predicatePushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *predicatePushdown) apply(root Node) bool {
	// collect filter nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Filter)
		return ok
	})

	changed := false
	for _, n := range nodes {
		filter := n.(*Filter)
		for i := 0; i < len(filter.Predicates); i++ {
			if !canApplyPredicate(filter.Predicates[i]) {
				continue
			}

			if ok := r.applyToTargets(filter, filter.Predicates[i]); ok {
				changed = true
				// remove predicates that have been pushed down
				filter.Predicates = slices.Delete(filter.Predicates, i, i+1)
				i--
			}
		}
	}

	return changed
}

func (r *predicatePushdown) applyToTargets(node Node, predicate Expression) bool {
	switch node := node.(type) {
	case *ScanSet:
		node.Predicates = append(node.Predicates, predicate)
		return true
	case *DataObjScan:
		node.Predicates = append(node.Predicates, predicate)
		return true
	}

	changed := false
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, predicate) {
			changed = true
		}
	}
	return changed
}

func canApplyPredicate(predicate Expression) bool {
	switch pred := predicate.(type) {
	case *BinaryExpr:
		return canApplyPredicate(pred.Left) && canApplyPredicate(pred.Right)
	case *ColumnExpr:
		return pred.Ref.Type == types.ColumnTypeBuiltin || pred.Ref.Type == types.ColumnTypeMetadata
	case *LiteralExpr:
		return true
	default:
		return false
	}
}
