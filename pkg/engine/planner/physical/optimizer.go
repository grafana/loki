package physical

import "github.com/grafana/loki/v3/pkg/engine/internal/types"

type optimizer struct {
	plan *Plan
}

var _ Visitor = (*optimizer)(nil)

// VisitDataObjScan implements Visitor.
func (o *optimizer) VisitDataObjScan(*DataObjScan) error {
	return nil
}

// VisitFilter implements Visitor.
func (o *optimizer) VisitFilter(node *Filter) error {
	for i := 0; i < len(node.Predicates); i++ {
		if ok := o.applyPredicatePushdown(node, node.Predicates[i]); ok {
			// remove predicates that have been pushed down
			node.Predicates = append(node.Predicates[:i], node.Predicates[i+1:]...)
			i--
		}
	}
	return nil
}

// VisitLimit implements Visitor.
func (o *optimizer) VisitLimit(node *Limit) error {
	o.applyLimitPushdown(node, node.Limit)
	return nil
}

// VisitProjection implements Visitor.
func (o *optimizer) VisitProjection(*Projection) error {
	return nil
}

// VisitSortMerge implements Visitor.
func (o *optimizer) VisitSortMerge(*SortMerge) error {
	return nil
}

func (o *optimizer) applyLimitPushdown(node Node, limit uint32) bool {
	switch node := node.(type) {
	case *DataObjScan:
		node.Limit = max(node.Limit, limit)
		return true
	}
	for _, child := range o.plan.Children(node) {
		if ok := o.applyLimitPushdown(child, limit); !ok {
			return ok
		}
	}
	return true
}

func (o *optimizer) applyPredicatePushdown(node Node, predicate Expression) bool {
	switch node := node.(type) {
	case *DataObjScan:
		if o.canApplyPredicate(predicate) {
			node.Predicates = append(node.Predicates, predicate)
			return true
		}
		return false
	}
	for _, child := range o.plan.Children(node) {
		if ok := o.applyPredicatePushdown(child, predicate); !ok {
			return ok
		}
	}
	return true
}

func (o *optimizer) canApplyPredicate(predicate Expression) bool {
	switch pred := predicate.(type) {
	case *BinaryExpr:
		return o.canApplyPredicate(pred.Left) && o.canApplyPredicate(pred.Right)
	case *ColumnExpr:
		return pred.ColumnType == types.ColumnTypeBuiltin || pred.ColumnType == types.ColumnTypeMetadata
	case *LiteralExpr:
		return true
	default:
		return false
	}
}
