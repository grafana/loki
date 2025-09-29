package physical

import (
	"maps"
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

// removeNoopMerge is a rule that removes merge/sortmerge nodes with only a single input
type removeNoopMerge struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopMerge) apply(node Node) bool {
	changed := false
	switch node := node.(type) {
	case *Merge, *SortMerge:
		if len(r.plan.Children(node)) <= 1 {
			r.plan.eliminateNode(node)
			changed = true
		}
	}
	return changed
}

var _ rule = (*removeNoopMerge)(nil)

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

// projectionPushdown is a rule that pushes down column projections.
// Currently it only projects partition labels from range aggregations to scan nodes.
type projectionPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *projectionPushdown) apply(node Node) bool {
	switch node := node.(type) {
	case *VectorAggregation:
		if len(node.GroupBy) == 0 || node.Operation != types.VectorAggregationTypeSum {
			return false
		}

		return r.pushToChildren(node, node.GroupBy, false)
	case *RangeAggregation:
		if len(node.PartitionBy) == 0 || node.Operation != types.RangeAggregationTypeCount {
			return false
		}

		projections := make([]ColumnExpression, len(node.PartitionBy)+1)
		copy(projections, node.PartitionBy)
		// Always project timestamp column
		projections[len(node.PartitionBy)] = &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}}

		return r.pushToChildren(node, projections, false)
	case *Filter:
		projections := extractColumnsFromPredicates(node.Predicates)
		if len(projections) == 0 {
			return false
		}

		// Filter nodes should only add their predicate columns to projections when
		// there's already a projection list in the plan (indicating a metric query).
		// For log queries that read all columns, filter columns should not be projected.
		//
		// Setting applyIfNotEmpty argument as true for this reason.
		return r.pushToChildren(node, projections, true)
	}

	return false
}

// applyProjectionPushdown applies the projection pushdown rule to the given node.
// we can't push all projections down to the scan node, since some may be referencing parsed columns.
// if applyIfNotEmpty is true, it will apply the projection pushdown only if the node has existing projections.
func (r *projectionPushdown) applyProjectionPushdown(
	node Node,
	projections []ColumnExpression,
	applyIfNotEmpty bool,
) bool {
	switch node := node.(type) {
	case *DataObjScan:
		return r.handleDataObjScan(node, projections, applyIfNotEmpty)
	case *ParseNode:
		return r.handleParseNode(node, projections, applyIfNotEmpty)
	case *RangeAggregation:
		return r.handleRangeAggregation(node, projections)
	case *Filter, *Merge, *SortMerge:
		// Push to next direct child that cares about projections
		return r.pushToChildren(node, projections, applyIfNotEmpty)
	}

	return false
}

// handleDataObjScan handles projection pushdown for DataObjScan nodes
func (r *projectionPushdown) handleDataObjScan(node *DataObjScan, projections []ColumnExpression, applyIfNotEmpty bool) bool {
	shouldNotApply := len(projections) == 0 && applyIfNotEmpty
	if !r.isMetricQuery() || shouldNotApply {
		return false
	}

	// Add to scan projections if not already present
	changed := false
	for _, colExpr := range projections {
		colExpr, ok := colExpr.(*ColumnExpr)
		if !ok {
			continue
		}

		var wasAdded bool
		node.Projections, wasAdded = addUniqueProjection(node.Projections, colExpr)
		if wasAdded {
			changed = true
		}
	}

	if changed {
		// Sort projections by column name for deterministic order
		slices.SortFunc(node.Projections, sortProjections)
	}

	return changed
}

// handleParseNode handles projection pushdown for ParseNode nodes
func (r *projectionPushdown) handleParseNode(node *ParseNode, projections []ColumnExpression, applyIfNotEmpty bool) bool {
	unambiguousProjections, ambiguousProjections := disambiguateColumns(projections)
	shouldNotApply := len(ambiguousProjections) == 0 && applyIfNotEmpty

	// Only apply the pushdown for Metric queries. Log queries should request all keys
	if !r.isMetricQuery() || shouldNotApply {
		return false
	}

	// Found a ParseNode - update its keys
	requestedKeys := make(map[string]bool)
	for _, k := range node.RequestedKeys {
		requestedKeys[k] = true
	}

	for _, p := range ambiguousProjections {
		colExpr, ok := p.(*ColumnExpr)
		if !ok {
			continue
		}

		// Only collect ambiguous columns to push to parse nodes
		if !requestedKeys[colExpr.Ref.Column] {
			requestedKeys[colExpr.Ref.Column] = true
		}
	}

	changed := len(requestedKeys) > len(node.RequestedKeys)
	if changed {
		// Convert back to sorted slice
		newKeys := slices.Collect(maps.Keys(requestedKeys))
		sort.Strings(newKeys)
		node.RequestedKeys = newKeys
	}

	projectionsToPushDown := make([]ColumnExpression, len(unambiguousProjections)+1)
	copy(projectionsToPushDown, unambiguousProjections)
	projectionsToPushDown[len(projectionsToPushDown)-1] = &ColumnExpr{
		Ref: types.ColumnRef{Column: types.ColumnNameBuiltinMessage, Type: types.ColumnTypeBuiltin},
	}

	// Push non-ambiguous projections down to children that care about them
	childrenChanged := r.pushToChildren(node, projectionsToPushDown, true)
	return changed || childrenChanged
}

// handleRangeAggregation handles projection pushdown for RangeAggregation nodes
func (r *projectionPushdown) handleRangeAggregation(node *RangeAggregation, projections []ColumnExpression) bool {
	if node.Operation != types.RangeAggregationTypeCount {
		return false
	}

	changed := false
	for _, colExpr := range projections {
		colExpr, ok := colExpr.(*ColumnExpr)
		if !ok {
			continue
		}

		var wasAdded bool
		node.PartitionBy, wasAdded = addUniqueProjection(node.PartitionBy, colExpr)
		if wasAdded {
			changed = true
		}
	}
	return changed
}

// pushToChildren is a helper method to push projections to all children of a node
func (r *projectionPushdown) pushToChildren(node Node, projections []ColumnExpression, applyIfNotEmpty bool) bool {
	var anyChanged bool
	for _, child := range r.plan.Children(node) {
		if changed := r.applyProjectionPushdown(child, projections, applyIfNotEmpty); changed {
			anyChanged = true
		}
	}
	return anyChanged
}

func sortProjections(a, b ColumnExpression) int {
	exprA, aOk := a.(*ColumnExpr)
	exprB, bOk := b.(*ColumnExpr)
	if !aOk || !bOk {
		return 0
	}

	if exprA.Ref.Column < exprB.Ref.Column {
		return -1
	}

	if exprA.Ref.Column > exprB.Ref.Column {
		return 1
	}

	return 0
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

// disambiguateColumns splits columns into ambiguous and unambiguous columns
func disambiguateColumns(columns []ColumnExpression) ([]ColumnExpression, []ColumnExpression) {
	ambiguousColumns := make([]ColumnExpression, 0, len(columns))
	unambiguousColumns := make([]ColumnExpression, 0, len(columns))
	for _, col := range columns {
		if colExpr, ok := col.(*ColumnExpr); ok {
			// Only collect ambiguous columns (might need parsing)
			// Skip labels (from stream selector) and builtins (like timestamp/message)
			if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
				ambiguousColumns = append(ambiguousColumns, col)
			} else {
				unambiguousColumns = append(unambiguousColumns, col)
			}
		}
	}

	return unambiguousColumns, ambiguousColumns
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

// addUniqueProjection adds a column to the projections list if it's not already present
func addUniqueProjection(projections []ColumnExpression, colExpr *ColumnExpr) ([]ColumnExpression, bool) {
	for _, existing := range projections {
		if existingCol, ok := existing.(*ColumnExpr); ok {
			if existingCol.Ref.Column == colExpr.Ref.Column {
				return projections, false // already exists
			}
		}
	}
	return append(projections, colExpr), true
}
