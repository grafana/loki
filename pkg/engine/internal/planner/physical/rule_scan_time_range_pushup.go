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
		dataObjScan, ok := n.(*DataObjScan)
		if ok {
			applied := r.applyToTargets(dataObjScan, dataObjScan.MaxTimeRange)
			if applied {
				changed = true
			}
		} else {
			pointersScan, ok := n.(*PointersScan)
			if ok {
				applied := r.applyToTargets(pointersScan, pointersScan.MaxTimeRange())
				if applied {
					changed = true
				}
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
		if node.Step == 0 {
			if node.Start.Compare(timeRange.Start) < 0 {
				node.Start = timeRange.Start
				changed = true
			}
			if node.End.Compare(timeRange.End) > 0 {
				node.End = timeRange.End
				changed = true
			}
		} else {
			trSteppedStart := time.UnixMilli((timeRange.Start.UnixMilli() / node.Step.Milliseconds()) * node.Step.Milliseconds()).UTC()

			endPlusRange := node.End.Add(node.Range)
			trSteppedEnd := time.UnixMilli((endPlusRange.UnixMilli() / node.Step.Milliseconds()) * node.Step.Milliseconds()).UTC()
			for trSteppedEnd.Compare(timeRange.End) < 0 {
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
