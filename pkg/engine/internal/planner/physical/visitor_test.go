package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
)

var _ Visitor = (*nodeCollectVisitor)(nil)

// A visitor implementation that collects nodes during traversal and optionally
// executes custom functions for each node type. Used primarily for testing
// traversal behavior.
type nodeCollectVisitor struct {
	visited                  []string
	onVisitDataObjScan       func(*DataObjScan) error
	onVisitFilter            func(*Filter) error
	onVisitLimit             func(*Limit) error
	onVisitProjection        func(*Projection) error
	onVisitRangeAggregation  func(*AggregateRange) error
	onVisitVectorAggregation func(*AggregateVector) error
	onVisitParse             func(*Parse) error
	onVisitParallelize       func(*Parallelize) error
	onVisitScanSet           func(*ScanSet) error
	onVisitJoin              func(*Join) error
	onVisitColumnCompat      func(*ColumnCompat) error
	onVisitMerge             func(*Merge) error
	onVisitSortMerge         func(*SortMerge) error
}

func (v *nodeCollectVisitor) VisitDataObjScan(n *DataObjScan) error {
	if v.onVisitDataObjScan != nil {
		return v.onVisitDataObjScan(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitFilter(n *Filter) error {
	if v.onVisitFilter != nil {
		return v.onVisitFilter(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitLimit(n *Limit) error {
	if v.onVisitLimit != nil {
		return v.onVisitLimit(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitProjection(n *Projection) error {
	if v.onVisitProjection != nil {
		return v.onVisitProjection(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitAggregateRange(n *AggregateRange) error {
	if v.onVisitRangeAggregation != nil {
		return v.onVisitRangeAggregation(n)
	}

	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitAggregateVector(n *AggregateVector) error {
	if v.onVisitVectorAggregation != nil {
		return v.onVisitVectorAggregation(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitParse(n *Parse) error {
	if v.onVisitParse != nil {
		return v.onVisitParse(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitCompat(*ColumnCompat) error {
	return nil
}

func (v *nodeCollectVisitor) VisitTopK(n *TopK) error {
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitParallelize(n *Parallelize) error {
	if v.onVisitParallelize != nil {
		return v.onVisitParallelize(n)
	}

	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitScanSet(n *ScanSet) error {
	if v.onVisitScanSet != nil {
		return v.onVisitScanSet(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitColumnCompat(n *ColumnCompat) error {
	if v.onVisitColumnCompat != nil {
		return v.onVisitColumnCompat(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitJoin(n *Join) error {
	if v.onVisitJoin != nil {
		return v.onVisitJoin(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}

func (v *nodeCollectVisitor) VisitMerge(n *Merge) error {
	if v.onVisitMerge != nil {
		return v.onVisitMerge(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}
func (v *nodeCollectVisitor) VisitSortMerge(n *SortMerge) error {
	if v.onVisitSortMerge != nil {
		return v.onVisitSortMerge(n)
	}
	v.visited = append(v.visited, fmt.Sprintf("%s.%s", n.Kind().String(), n.ID()))
	return nil
}
