package physical

import (
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var _ rule = (*clampPredicates)(nil)

// clampPredicates is a rule that clamps the time predicates to the time range of the scan nodes.
// For example, if a scan node has a BinaryExpr predicate of timestamp <= 2026-03-30 14:20:00
// and the max time range ends at 2026-03-30 14:15:00,
// we adjust the predicate to timestamp <= 2026-03-30 14:15:00.
type clampPredicates struct {
	plan *Plan
}

// apply implements rule.
func (r *clampPredicates) apply(root Node) bool {
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
	var maxTimeRange TimeRange
	var nodeChanged bool
	for _, n := range nodes {
		switch n := n.(type) {
		case *DataObjScan:
			n.Predicates, nodeChanged = r.clamp(n.Predicates, n.MaxTimeRange)
			if nodeChanged {
				changed = true
			}
			maxTimeRange = n.MaxTimeRange
		case *PointersScan:
			n.Predicates, nodeChanged = r.clamp(n.Predicates, n.MaxTimeRange())
			if nodeChanged {
				changed = true
			}
			maxTimeRange = n.MaxTimeRange()
		}
	}
	if len(nodes) > 1 {
		//can't optimize across other nodes
		//if there are multiple scan nodes with their own time ranges
		return changed
	}
	for _, n := range nodes {
		if r.applyToTargets(n, maxTimeRange) {
			changed = true
		}
	}
	return changed
}

func (r *clampPredicates) applyToTargets(node Node, maxTimeRange TimeRange) bool {
	var changed bool
	switch node := node.(type) {
	case *Filter:
		node.Predicates, changed = r.clamp(node.Predicates, maxTimeRange)
	}

	// Continue to parents
	for _, parent := range r.plan.Parent(node) {
		if r.applyToTargets(parent, maxTimeRange) {
			changed = true
		}
	}
	return changed
}

func (r *clampPredicates) clamp(predicates []Expression, timeRange TimeRange) ([]Expression, bool) {
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
func (r *clampPredicates) validateAsClampable(e *BinaryExpr) (types.Timestamp, bool) {
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
func (r *clampPredicates) clampExpression(e Expression, tr TimeRange) (Expression, bool) {
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
