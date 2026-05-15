package physical

import (
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// clampTimeRangesToScan is a rule that moves the max time range from scan nodes up to RangeAggregations.
type clampTimeRangesToScan struct {
	plan *Plan
}

// apply implements rule.
func (r *clampTimeRangesToScan) apply(root Node) bool {
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

	changed := false

	var nodeChanged bool
	for _, n := range nodes {
		switch n := n.(type) {
		case *DataObjScan:
			n.Predicates, nodeChanged = r.clamp(n.Predicates, n.MaxTimeRange)
			if nodeChanged {
				changed = true
			}
		case *PointersScan:
			n.Predicates, nodeChanged = r.clamp(n.Predicates, n.MaxTimeRange())
			if nodeChanged {
				changed = true
			}
		}
	}

	// propagate time range to target parent nodes.
Loop:
	for _, n := range nodes {
		switch scan := n.(type) {
		case *DataObjScan:
			if r.applyToTargets(scan, scan.MaxTimeRange) {
				changed = true
				break Loop // should be at most one scan node, so break after applying to the first one.
			}
		case *PointersScan:
			if r.applyToTargets(scan, scan.MaxTimeRange()) {
				changed = true
				break Loop // should be at most one scan node, so break after applying to the first one.
			}
		}
	}
	return changed
}

// applyToTargets applies max time range on target nodes.
func (r *clampTimeRangesToScan) applyToTargets(node Node, timeRange TimeRange) bool {
	var changed bool
	switch node := node.(type) {
	case *RangeAggregation:
		if node.Step > 0 { // only apply optimization to range queries
			trSteppedStart := time.UnixMilli((timeRange.Start.UnixNano() / node.Step.Nanoseconds()) * node.Step.Nanoseconds() / 1000000).UTC()

			endPlusRange := timeRange.End.Add(node.Range)
			trSteppedEnd := time.UnixMilli((endPlusRange.UnixNano() / node.Step.Nanoseconds()) * node.Step.Nanoseconds() / 1000000).UTC()
			if trSteppedEnd.Compare(endPlusRange) < 0 {
				steps := endPlusRange.Sub(trSteppedEnd)/node.Step + 1
				trSteppedEnd = trSteppedEnd.Add(steps * node.Step)
			}
			if node.Start.Compare(trSteppedStart) < 0 {
				node.Start = trSteppedStart
				changed = true
			}
			// trSteppedEnd could still be before node.Start; make sure it isn't
			if trSteppedEnd.Compare(node.Start) <= 0 {
				steps := node.Start.Sub(trSteppedEnd)/node.Step + 1
				trSteppedEnd = trSteppedEnd.Add(steps * node.Step)
			}

			if node.End.Compare(trSteppedEnd) > 0 {
				node.End = trSteppedEnd
				changed = true
			}
		}
	case *Filter:
		node.Predicates, changed = r.clamp(node.Predicates, timeRange)
	}

	// Continue to parents
	for _, parent := range r.plan.Parent(node) {
		if r.applyToTargets(parent, timeRange) {
			changed = true
		}
	}
	return changed
}

func (r *clampTimeRangesToScan) clamp(predicates []Expression, timeRange TimeRange) ([]Expression, bool) {
	newPredicates := make([]Expression, len(predicates))

	var changed bool
	for i, p := range predicates {
		var clamped bool
		newPredicates[i], clamped = r.clampExpression(p, timeRange)
		if clamped {
			changed = true
		}
	}
	return newPredicates, changed
}

// Returns the timestamp and true for BinaryExpressions that take the form ">= timestamp", "<= timestamp", "> timestamp", or "< timestamp".
// Otherwise returns a default timestamp of 0 and false.
func (r *clampTimeRangesToScan) validateAsClampable(e *BinaryExpr) (types.Timestamp, bool) {
	col, ok := e.Left.(*ColumnExpr)
	if !ok || col.Ref.Column != types.ColumnNameBuiltinTimestamp || col.Ref.Type != types.ColumnTypeBuiltin {
		// Expression is not checking the timestamp column
		return types.Timestamp(0), false
	}

	lit, ok := e.Right.(*LiteralExpr)
	if !ok || lit.ValueType() != types.Loki.Timestamp {
		// Expression does not use a timestamp
		return types.Timestamp(0), false
	}

	ts, ok := lit.Value().(types.Timestamp)
	if !ok {
		// Failure to cast timestamp literal value
		return types.Timestamp(0), false
	}
	return ts, true
}

// Only modifies BinaryExpressions that take the form ">= timestamp", "<= timestamp", "> timestamp", or "< timestamp".
// Returns true if the expression has been modified or false if it has not.
func (r *clampTimeRangesToScan) clampExpression(e Expression, tr TimeRange) (Expression, bool) {
	switch e := e.(type) {
	case *BinaryExpr:
		ts, ok := r.validateAsClampable(e)
		if !ok {
			return e, false
		}
		if tr.IsZero() {
			return e, false
		}

		t2 := time.Unix(0, int64(ts))
		orig := ts
		newOp := e.Op
		switch e.Op {
		case types.BinaryOpGte, types.BinaryOpGt:
			// When clamping "time > max" we switch to "time >= max" because ">" should be exclusive.
			if t2.Before(tr.Start) {
				ts = types.Timestamp(tr.Start.UnixNano())
				newOp = types.BinaryOpGte
			}
		case types.BinaryOpLte, types.BinaryOpLt:
			// When clamping "time < max" we switch to "time <= max" because "<" should be exclusive.
			if t2.After(tr.End) {
				ts = types.Timestamp(tr.End.UnixNano())
				newOp = types.BinaryOpLte
			}
		}
		if ts == orig {
			return e, false
		}
		return &BinaryExpr{
			Left:  e.Left,
			Right: NewLiteral(ts),
			Op:    newOp,
		}, true

	default:
		return e, false
	}
}
