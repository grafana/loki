package physical

type Traversable interface {
	Accept(Visitor) (bool, error)
}

type Visitor interface {
	VisitDataObjScan(*DataObjScan) (bool, error)
	VisitSortMerge(*SortMerge) (bool, error)
	VisitProjection(*Projection) (bool, error)
	VisitFilter(*Filter) (bool, error)
	VisitLimit(*Limit) (bool, error)
}

var _ Visitor = (*DepthFirstTraversal)(nil)

type DepthFirstTraversal struct {
	impl Visitor
}

func (v *DepthFirstTraversal) visitChildren(plan Node) (bool, error) {
	for _, child := range plan.Children() {
		if _, err := child.Accept(v); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (v *DepthFirstTraversal) VisitDataObjScan(plan *DataObjScan) (bool, error) {
	if _, err := v.visitChildren(plan); err != nil {
		return false, err
	}
	return v.impl.VisitDataObjScan(plan)
}

func (v *DepthFirstTraversal) VisitSortMerge(plan *SortMerge) (bool, error) {
	if _, err := v.visitChildren(plan); err != nil {
		return false, err
	}
	return v.impl.VisitSortMerge(plan)
}

func (v *DepthFirstTraversal) VisitProjection(plan *Projection) (bool, error) {
	if _, err := v.visitChildren(plan); err != nil {
		return false, err
	}
	return v.impl.VisitProjection(plan)
}

func (v *DepthFirstTraversal) VisitFilter(plan *Filter) (bool, error) {
	if _, err := v.visitChildren(plan); err != nil {
		return false, err
	}
	return v.impl.VisitFilter(plan)
}

func (v *DepthFirstTraversal) VisitLimit(plan *Limit) (bool, error) {
	if _, err := v.visitChildren(plan); err != nil {
		return false, err
	}
	return v.impl.VisitLimit(plan)
}
