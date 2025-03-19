package physical

import (
	"fmt"
)

// A visitor implementation that collects nodes during traversal and optionally
// executes custom functions for each node type. Used primarily for testing
// traversal behavior.
type nodeCollectVisitor struct {
	visited            []string
	onVisitDataObjScan func(*DataObjScan) error
	onVisitFilter      func(*Filter) error
	onVisitLimit       func(*Limit) error
	onVisitSortMerge   func(*SortMerge) error
	onVisitProjection  func(*Projection) error
}

func (v *nodeCollectVisitor) VisitDataObjScan(n *DataObjScan) error {
	if v.onVisitDataObjScan != nil {
		return v.onVisitDataObjScan(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Type().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitFilter(n *Filter) error {
	if v.onVisitFilter != nil {
		return v.onVisitFilter(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Type().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitLimit(n *Limit) error {
	if v.onVisitLimit != nil {
		return v.onVisitLimit(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Type().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitProjection(n *Projection) error {
	if v.onVisitProjection != nil {
		return v.onVisitProjection(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Type().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitSortMerge(n *SortMerge) error {
	if v.onVisitSortMerge != nil {
		return v.onVisitSortMerge(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Type().String(), n.ID()))
	return nil
}
