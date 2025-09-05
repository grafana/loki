package physical

import (
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// A rule is a tranformation that can be applied on a Node.
type rule interface {
	// apply tries to apply the transformation on the node.
	// It returns a boolean indicating whether the transformation has been applied.
	apply(Node) bool
}

// removeNoopFilter is a rule that removes Filter nodes without predicates.
type removeNoopFilter struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopFilter) apply(node Node) bool {
	changed := false
	switch node := node.(type) {
	case *Filter:
		if len(node.Predicates) == 0 {
			r.plan.eliminateNode(node)
			changed = true
		}
	}
	return changed
}

var _ rule = (*removeNoopFilter)(nil)

// predicatePushdown is a rule that moves down filter predicates to the scan nodes.
type predicatePushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *predicatePushdown) apply(node Node) bool {
	changed := false
	switch node := node.(type) {
	case *Filter:
		for i := 0; i < len(node.Predicates); i++ {
			if ok := r.applyPredicatePushdown(node, node.Predicates[i]); ok {
				changed = true
				// remove predicates that have been pushed down
				node.Predicates = slices.Delete(node.Predicates, i, i+1)
				i--
			}
		}
	}
	return changed
}

func (r *predicatePushdown) applyPredicatePushdown(node Node, predicate Expression) bool {
	switch node := node.(type) {
	case *DataObjScan:
		if canApplyPredicate(predicate) {
			node.Predicates = append(node.Predicates, predicate)
			return true
		}
		return false
	}
	for _, child := range r.plan.Children(node) {
		if ok := r.applyPredicatePushdown(child, predicate); !ok {
			return false
		}
	}
	return true
}

func canApplyPredicate(predicate Expression) bool {
	switch pred := predicate.(type) {
	case *BinaryExpr:
		return canApplyPredicate(pred.Left) && canApplyPredicate(pred.Right)
	case *ColumnExpr:
		return pred.Ref.Type == types.ColumnTypeBuiltin || pred.Ref.Type == types.ColumnTypeMetadata
	case *LiteralExpr:
		return true
	default:
		return false
	}
}

var _ rule = (*predicatePushdown)(nil)

// limitPushdown is a rule that moves down the limit to the scan nodes.
type limitPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *limitPushdown) apply(node Node) bool {
	switch node := node.(type) {
	case *Limit:
		return r.applyLimitPushdown(node, node.Fetch)
	}
	return false
}

func (r *limitPushdown) applyLimitPushdown(node Node, limit uint32) bool {
	switch node := node.(type) {
	case *DataObjScan:
		// In case the scan node is reachable from multiple different limit nodes, we need to take the largest limit.
		node.Limit = max(node.Limit, limit)
		return true
	case *Filter:
		// If there is a filter, child nodes may need to read up to all their lines to successfully apply the filter, so stop applying limit pushdown.
		return false
	}

	var changed bool
	for _, child := range r.plan.Children(node) {
		if ok := r.applyLimitPushdown(child, limit); ok {
			changed = true
		}
	}
	return changed
}

var _ rule = (*limitPushdown)(nil)

// groupByPushdown is a rule that pushes down grouping keys from vector aggregations to range aggregations.
type groupByPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *groupByPushdown) apply(node Node) bool {
	switch node := node.(type) {
	case *VectorAggregation:
		if node.Operation != types.VectorAggregationTypeSum {
			return false
		}

		return r.applyGroupByPushdown(node, node.GroupBy)
	}

	return false
}

func (r *groupByPushdown) applyGroupByPushdown(node Node, groupBy []ColumnExpression) bool {
	switch node := node.(type) {
	case *RangeAggregation:
		if node.Operation != types.RangeAggregationTypeCount {
			return false
		}

		// Push down the grouping labels to the range aggregation
		changed := false
		for _, colExpr := range groupBy {
			colExpr, ok := colExpr.(*ColumnExpr)
			if !ok {
				continue
			}

			found := false
			for _, existingCol := range node.PartitionBy {
				existingCol, ok := existingCol.(*ColumnExpr)
				if ok && existingCol.Ref.Column == colExpr.Ref.Column {
					found = true
					break
				}
			}
			if !found {
				node.PartitionBy = append(node.PartitionBy, colExpr)
				changed = true
			}
		}
		return changed
	}

	anyChanged := false
	for _, child := range r.plan.Children(node) {
		if changed := r.applyGroupByPushdown(child, groupBy); changed {
			anyChanged = true
		}
	}
	return anyChanged
}

var _ rule = (*groupByPushdown)(nil)

// projectionPushdown is a rule that pushes down column projections.
// Currently it only projects partition labels from range aggregations to scan nodes.
type projectionPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *projectionPushdown) apply(node Node) bool {
	switch node := node.(type) {
	case *RangeAggregation:
		if len(node.PartitionBy) == 0 || node.Operation != types.RangeAggregationTypeCount {
			return false
		}

		scanProjections := make([]ColumnExpression, len(node.PartitionBy)+1)
		copy(scanProjections, node.PartitionBy)
		// Always project timestamp column
		scanProjections[len(node.PartitionBy)] = &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}}

		additionalProjections := collectAmbiguousColumns(node.PartitionBy)
		return r.applyProjectionPushdown(node, scanProjections, additionalProjections, false)
	case *VectorAggregation:
		if len(node.GroupBy) == 0 || node.Operation != types.VectorAggregationTypeSum {
			return false
		}

		scanProjections := make([]ColumnExpression, len(node.GroupBy)+1)
		copy(scanProjections, node.GroupBy)
		// Always project timestamp column
		scanProjections[len(node.GroupBy)] = &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}}
		additionalProjections := collectAmbiguousColumns(node.GroupBy)

		return r.applyProjectionPushdown(node, scanProjections, additionalProjections, false)

	case *ParseNode:
		// Parse nodes need the message column to extract fields from log lines
		scanProjections := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinMessage, Type: types.ColumnTypeBuiltin}},
		}
		// Apply projection pushdown, but only add to existing projections (for metric queries)
		return r.applyProjectionPushdown(node, scanProjections, nil, true)
	case *Filter:
		scanProjections := extractColumnsFromPredicates(node.Predicates)
		if len(scanProjections) == 0 {
			return false
		}
		additionalProjections := collectAmbiguousColumns(extractColumnsFromPredicates(node.Predicates))

		// Filter nodes should only add their predicate columns to projections when
		// there's already a projection list in the plan (indicating a metric query).
		// For log queries that read all columns, filter columns should not be projected.
		//
		// Setting applyIfNotEmpty argument as true for this reason.
		return r.applyProjectionPushdown(node, scanProjections, additionalProjections, true)
	}
	return false
}

// applyProjectionPushdown applies the projection pushdown rule to the given node.
// we can't push all projections down to the scan node, since some may be referencing parsed columns.
// if applyIfNotEmpty is true, it will apply the projection pushdown only if the node has existing projections.
func (r *projectionPushdown) applyProjectionPushdown(
	node Node,
	scanProjections []ColumnExpression,
	additionalProjections []ColumnExpression,
	applyIfNotEmpty bool,
) bool {
	switch node := node.(type) {
	case *DataObjScan:
		if len(node.Projections) == 0 && applyIfNotEmpty {
			return false
		}

		// Add to scan projections if not already present
		changed := false
		for _, colExpr := range scanProjections {
			colExpr, ok := colExpr.(*ColumnExpr)
			if !ok {
				continue
			}

			// Check if this column is already in projections
			found := false
			for _, existingCol := range node.Projections {
				existingCol, ok := existingCol.(*ColumnExpr)
				if ok && existingCol.Ref.Column == colExpr.Ref.Column {
					found = true
					break
				}
			}

			if !found {
				node.Projections = append(node.Projections, colExpr)
				changed = true
			}
		}
		return changed
	case *ParseNode:
		// Only apply the pushdown for Metric queries. Log queries should request all keys
		if !r.isMetricQuery() || len(additionalProjections) == 0 {
			return false
		}

		// Found a ParseNode - update its keys
		existingKeys := make(map[string]bool)
		for _, k := range node.RequestedKeys {
			existingKeys[k] = true
		}

		// Add new keys
		changed := false

		for _, p := range additionalProjections {
			colExpr, ok := p.(*ColumnExpr)
			if !ok {
				continue
			}

			// Only collect ambiguous columns to push to parse nodes
			if colExpr.Ref.Type == types.ColumnTypeAmbiguous && !existingKeys[colExpr.Ref.Column] {
				existingKeys[colExpr.Ref.Column] = true
				changed = true
			}
		}

		if changed {
			// Convert back to sorted slice
			newKeys := make([]string, 0, len(existingKeys))
			for k := range existingKeys {
				newKeys = append(newKeys, k)
			}
			sort.Strings(newKeys)
			node.RequestedKeys = newKeys
		}
		return changed
	}

	anyChanged := false
	for _, child := range r.plan.Children(node) {
		if changed := r.applyProjectionPushdown(child, scanProjections, additionalProjections, applyIfNotEmpty); changed {
			anyChanged = true
		}
	}
	return anyChanged
}

// isMetricQuery checks if the plan contains a RangeAggregation or VectorAggregation node, indicating a metric query
func (r *projectionPushdown) isMetricQuery() bool {
	for node := range r.plan.nodes {
		if _, ok := node.(*RangeAggregation); ok {
			return true
		}
		if _, ok := node.(*VectorAggregation); ok {
			return true
		}
	}
	return false
}

var _ rule = (*projectionPushdown)(nil)

// collectAmbiguousColumns filters columns down to only those with ColumnTypeAmbiguous
func collectAmbiguousColumns(columns []ColumnExpression) []ColumnExpression {
	result := make([]ColumnExpression, 0, len(columns))
	for _, col := range columns {
		if colExpr, ok := col.(*ColumnExpr); ok {
			// Only collect ambiguous columns (might need parsing)
			// Skip labels (from stream selector) and builtins (like timestamp/message)
			if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
				result = append(result, col)
			}
		}
	}

	return result
}

// optimization represents a single optimization pass and can hold multiple rules.
type optimization struct {
	plan  *Plan
	name  string
	rules []rule
}

func newOptimization(name string, plan *Plan) *optimization {
	return &optimization{
		name: name,
		plan: plan,
	}
}

func (o *optimization) withRules(rules ...rule) *optimization {
	o.rules = append(o.rules, rules...)
	return o
}

func (o *optimization) optimize(node Node) {
	iterations, maxIterations := 0, 3

	for iterations < maxIterations {
		iterations++

		if !o.applyRules(node) {
			// Stop immediately if an optimization pass produced no changes.
			break
		}
	}
}

func (o *optimization) applyRules(node Node) bool {
	anyChanged := false

	for _, child := range o.plan.Children(node) {
		changed := o.applyRules(child)
		if changed {
			anyChanged = true
		}
	}

	for _, rule := range o.rules {
		changed := rule.apply(node)
		if changed {
			anyChanged = true
		}
	}

	return anyChanged
}

// The optimizer can optimize physical plans using the provided optimization passes.
type optimizer struct {
	plan          *Plan
	optimisations []*optimization
}

func newOptimizer(plan *Plan, passes []*optimization) *optimizer {
	return &optimizer{plan: plan, optimisations: passes}
}

func (o *optimizer) optimize(node Node) {
	for _, optimisation := range o.optimisations {
		optimisation.optimize(node)
	}
}

func extractColumnsFromPredicates(predicates []Expression) []ColumnExpression {
	columns := make([]ColumnExpression, 0, len(predicates))
	for _, p := range predicates {
		extractColumnsFromExpression(p, &columns)
	}

	return deduplicateColumns(columns)
}

func extractColumnsFromExpression(expr Expression, columns *[]ColumnExpression) {
	switch e := expr.(type) {
	case *ColumnExpr:
		*columns = append(*columns, e)
	case *BinaryExpr:
		extractColumnsFromExpression(e.Left, columns)
		extractColumnsFromExpression(e.Right, columns)
	case *UnaryExpr:
		extractColumnsFromExpression(e.Left, columns)
	default:
		// Ignore other expression types
	}
}

func deduplicateColumns(columns []ColumnExpression) []ColumnExpression {
	seen := make(map[string]bool)
	var result []ColumnExpression

	for _, col := range columns {
		if colExpr, ok := col.(*ColumnExpr); ok {
			key := colExpr.Ref.Column
			if !seen[key] {
				seen[key] = true
				result = append(result, col)
			}
		}
	}

	return result
}
