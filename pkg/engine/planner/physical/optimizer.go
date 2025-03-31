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
		v := &predicatePushdownVisitor{
			predicate: node.Predicates[i],
		}
		node.Accept(v)
		if v.ok {
			// remove predicates that have been pushed down
			node.Predicates = append(node.Predicates[:i], node.Predicates[i+1:]...)
			i--
		}
	}
	return nil
}

// VisitLimit implements Visitor.
func (o *optimizer) VisitLimit(node *Limit) error {
	v := &limitPushdownVisitor{
		limit: node.Limit,
	}
	node.Accept(v)
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

type limitPushdownVisitor struct {
	ok    bool
	limit uint32
}

// VisitDataObjScan implements Visitor.
func (p *limitPushdownVisitor) VisitDataObjScan(node *DataObjScan) error {
	p.ok = true
	node.Limit = p.limit
	return nil
}

// VisitFilter implements Visitor.
func (p *limitPushdownVisitor) VisitFilter(*Filter) error {
	return nil
}

// VisitLimit implements Visitor.
func (p *limitPushdownVisitor) VisitLimit(*Limit) error {
	return nil
}

// VisitProjection implements Visitor.
func (p *limitPushdownVisitor) VisitProjection(*Projection) error {
	return nil
}

// VisitSortMerge implements Visitor.
func (p *limitPushdownVisitor) VisitSortMerge(*SortMerge) error {
	return nil
}

var _ Visitor = (*limitPushdownVisitor)(nil)

type predicatePushdownVisitor struct {
	ok        bool
	predicate Expression
}

// VisitDataObjScan implements Visitor.
func (p *predicatePushdownVisitor) VisitDataObjScan(node *DataObjScan) error {
	p.ok = canApplyPredicate(p.predicate)
	if p.ok {
		node.Predicates = append(node.Predicates, p.predicate)
	}
	return nil
}

// VisitFilter implements Visitor.
func (p *predicatePushdownVisitor) VisitFilter(*Filter) error {
	return nil
}

// VisitLimit implements Visitor.
func (p *predicatePushdownVisitor) VisitLimit(*Limit) error {
	return nil
}

// VisitProjection implements Visitor.
func (p *predicatePushdownVisitor) VisitProjection(*Projection) error {
	return nil
}

// VisitSortMerge implements Visitor.
func (p *predicatePushdownVisitor) VisitSortMerge(*SortMerge) error {
	return nil
}

var _ Visitor = (*predicatePushdownVisitor)(nil)
