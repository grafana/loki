package physical

import (
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var _ rule = (*groupByPushdown)(nil)

// groupByPushdown is an optimisation rule that enables groupby labels to be pushed down to range aggregations.
type groupByPushdown struct {
	plan *Plan
}

func (r *groupByPushdown) apply(root Node) bool {
	nodes := findMatchingNodes(r.plan, root, func(n Node) bool {
		_, ok := n.(*VectorAggregation)
		return ok
	})

	var changed bool
	for _, n := range nodes {
		vecAgg := n.(*VectorAggregation)

		if vecAgg.Grouping.Without && len(vecAgg.Grouping.Columns) == 0 {
			// No changes required: Empty without() grouping will aggregation over all possible columns, so it cannot make a child column set stricter.
			continue
		} else if vecAgg.Grouping.Without {
			// TODO: Support pushing down VectorAggregation without(X) groupings
			continue
		}

		// Pushing down groupBy is valid only for certain combinations as these are both commutative and associative.
		// SUM -> SUM, COUNT
		// MAX -> MAX
		// MIN -> MIN
		var supportedAggTypes []types.RangeAggregationType
		switch vecAgg.Operation {
		case types.VectorAggregationTypeSum:
			supportedAggTypes = append(supportedAggTypes, types.RangeAggregationTypeSum, types.RangeAggregationTypeCount)
		case types.VectorAggregationTypeMax:
			supportedAggTypes = append(supportedAggTypes, types.RangeAggregationTypeMax)
		case types.VectorAggregationTypeMin:
			supportedAggTypes = append(supportedAggTypes, types.RangeAggregationTypeMin)
		default:
			return false
		}

		if r.applyToTargets(vecAgg, vecAgg.Grouping.Columns, supportedAggTypes...) {
			changed = true
		}
	}

	return changed
}

func (r *groupByPushdown) applyToTargets(node Node, grouping []ColumnExpression, supportedAggTypes ...types.RangeAggregationType) bool {
	var changed bool
	switch node := node.(type) {
	case *RangeAggregation:
		if !slices.Contains(supportedAggTypes, node.Operation) {
			return false
		}

		if node.Grouping.Without && len(node.Grouping.Columns) > 0 {
			// TODO: Add support for computing the strictest column set in Without(X) cases.
			return false
		}

		for _, colExpr := range grouping {
			colExpr, ok := colExpr.(*ColumnExpr)
			if !ok {
				continue
			}

			var wasAdded bool
			node.Grouping.Columns, wasAdded = addUniqueColumnExpr(node.Grouping.Columns, colExpr)
			if wasAdded {
				node.Grouping.Without = false
				changed = true
			}
		}

		if len(grouping) == 0 {
			// RangeAggregation with empty without(), meaning all columns, can always be overridden by a stricter column set from the parent.
			node.Grouping.Without = false
			changed = true
		}

		return changed
	}

	// Continue to children
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, grouping, supportedAggTypes...) {
			changed = true
		}
	}

	return changed
}
