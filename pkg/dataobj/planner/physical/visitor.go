package physical

import "fmt"

type Acceptor interface {
	Accept(Visitor) (bool, error)
}

type Visitor interface {
	VisitDataObjScan(*DataObjScan) (bool, error)
	VisitLimit(*Limit) (bool, error)
	VisitFilter(*Filter) (bool, error)
	VisitSortMerge(*SortMerge) (bool, error)
}

var _ Visitor = (*RootVisitor)(nil)

type RootVisitor struct{}

func (v *RootVisitor) Accept(p Node) (bool, error) {
	switch plan := p.(type) {
	case *DataObjScan:
		return v.VisitDataObjScan(plan)
	case *Limit:
		return v.VisitLimit(plan)
	case *Filter:
		return v.VisitFilter(plan)
	case *SortMerge:
		return v.VisitSortMerge(plan)
	default:
		panic(fmt.Sprintf("supported plan type: %T", plan))
	}
}

func (v *RootVisitor) visitGeneric(plan Node) (bool, error) {
	for _, child := range plan.Children() {
		cont, err := v.Accept(child)
		if !cont {
			return cont, err
		}
	}
	return true, nil
}

func (v *RootVisitor) VisitDataObjScan(plan *DataObjScan) (bool, error) {
	return v.visitGeneric(plan)
}

func (v *RootVisitor) VisitLimit(plan *Limit) (bool, error) {
	return v.visitGeneric(plan)
}

func (v *RootVisitor) VisitFilter(plan *Filter) (bool, error) {
	return v.visitGeneric(plan)
}

func (v *RootVisitor) VisitSortMerge(plan *SortMerge) (bool, error) {
	return v.visitGeneric(plan)
}
