package physical

import "time"

// scanTimeRangePushup is a rule that moves the max time range from scan nodes up to RangeAggregations.
type scanTimeRangePushup struct {
	plan *Plan
}

// apply implements rule.
func (r *scanTimeRangePushup) apply(root Node) bool {
	// collect scan nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		switch node.Type() {
		case NodeTypeDataObjScan:
			return true
		case NodeTypePointersScan:
			return true
		}
		return false
	})

	// propagate time range to target parent nodes.
	changed := false
	for _, n := range nodes {
		switch scan := n.(type) {
		case *DataObjScan:
			applied := r.applyToTargets(scan, scan.MaxTimeRange)
			if applied {
				changed = true
				break // should be at most one scan node, so break after applying to the first one.
			}
		case *PointersScan:
			applied := r.applyToTargets(scan, scan.MaxTimeRange())
			if applied {
				changed = true
				break // should be at most one scan node, so break after applying to the first one.
			}
		}
	}
	return changed
}

// applyToTargets applies max time range on target nodes.
func (r *scanTimeRangePushup) applyToTargets(node Node, timeRange TimeRange) bool {
	var changed bool
	switch node := node.(type) {
	case *RangeAggregation:
		if node.Step != 0 { // only apply optimization to range queries
			trSteppedStart := time.UnixMilli((timeRange.Start.UnixMilli() / node.Step.Milliseconds()) * node.Step.Milliseconds()).UTC()

			endPlusRange := timeRange.End.Add(node.Range)
			trSteppedEnd := time.UnixMilli((endPlusRange.UnixMilli() / node.Step.Milliseconds()) * node.Step.Milliseconds()).UTC()
			for trSteppedEnd.Compare(endPlusRange) < 0 {
				trSteppedEnd = trSteppedEnd.Add(node.Step)
			}
			if node.Start.Compare(trSteppedStart) < 0 {
				node.Start = trSteppedStart
				changed = true
			}
			// trSteppedEnd could still be before node.Start; make sure it isn't
			for trSteppedEnd.Compare(node.Start) <= 0 {
				trSteppedEnd = trSteppedEnd.Add(node.Step)
			}

			if node.End.Compare(trSteppedEnd) > 0 {
				node.End = trSteppedEnd
				changed = true
			}
		}
	}

	// Continue to parents
	for _, parent := range r.plan.Parent(node) {
		if r.applyToTargets(parent, timeRange) {
			changed = true
		}
	}
	return changed
}
