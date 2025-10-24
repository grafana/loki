package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
)

var _ physicalpb.Visitor = (*nodeCollectVisitor)(nil)

// A visitor implementation that collects nodes during traversal and optionally
// executes custom functions for each node type. Used primarily for testing
// traversal behavior.
type nodeCollectVisitor struct {
	visited                []string
	onVisitDataObjScan     func(*physicalpb.DataObjScan) error
	onVisitFilter          func(*physicalpb.Filter) error
	onVisitLimit           func(*physicalpb.Limit) error
	onVisitProjection      func(*physicalpb.Projection) error
	onVisitAggregateRange  func(*physicalpb.AggregateRange) error
	onVisitAggregateVector func(*physicalpb.AggregateVector) error
	onVisitParse           func(*physicalpb.Parse) error
	onVisitColumnCompat    func(*physicalpb.ColumnCompat) error
	onVisitParallelize     func(*physicalpb.Parallelize) error
	onVisitScanSet         func(*physicalpb.ScanSet) error
	onVisitMerge           func(*physicalpb.Merge) error
	onVisitSortMerge       func(*physicalpb.SortMerge) error
}

func (v *nodeCollectVisitor) VisitDataObjScan(n *physicalpb.DataObjScan) error {
	if v.onVisitDataObjScan != nil {
		return v.onVisitDataObjScan(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitFilter(n *physicalpb.Filter) error {
	if v.onVisitFilter != nil {
		return v.onVisitFilter(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitLimit(n *physicalpb.Limit) error {
	if v.onVisitLimit != nil {
		return v.onVisitLimit(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitProjection(n *physicalpb.Projection) error {
	if v.onVisitProjection != nil {
		return v.onVisitProjection(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitAggregateRange(n *physicalpb.AggregateRange) error {
	if v.onVisitAggregateRange != nil {
		return v.onVisitAggregateRange(n)
	}

	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitAggregateVector(n *physicalpb.AggregateVector) error {
	if v.onVisitAggregateVector != nil {
		return v.onVisitAggregateVector(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitParse(n *physicalpb.Parse) error {
	if v.onVisitParse != nil {
		return v.onVisitParse(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitCompat(*physicalpb.ColumnCompat) error {
	return nil
}

func (v *nodeCollectVisitor) VisitTopK(n *physicalpb.TopK) error {
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitParallelize(n *physicalpb.Parallelize) error {
	if v.onVisitParallelize != nil {
		return v.onVisitParallelize(n)
	}

	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitScanSet(n *physicalpb.ScanSet) error {
	if v.onVisitScanSet != nil {
		return v.onVisitScanSet(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitColumnCompat(n *physicalpb.ColumnCompat) error {
	if v.onVisitColumnCompat != nil {
		return v.onVisitColumnCompat(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitMerge(n *physicalpb.Merge) error {
	if v.onVisitMerge != nil {
		return v.onVisitMerge(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitSortMerge(n *physicalpb.SortMerge) error {
	if v.onVisitMerge != nil {
		return v.onVisitSortMerge(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}
