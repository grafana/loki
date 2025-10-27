package physical

import (
	"maps"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// A rule is a transformation that can be applied on a Node.
type rule interface {
	// apply tries to apply the transformation on the node.
	// It returns a boolean indicating whether the transformation has been applied.
	apply(physicalpb.Node) bool
}

// removeNoopFilter is a rule that removes Filter nodes without predicates.
type removeNoopFilter struct {
	plan *physicalpb.Plan
}

// apply implements rule.
func (r *removeNoopFilter) apply(node physicalpb.Node) bool {
	changed := false
	switch node := node.(type) {
	case *physicalpb.Filter:
		if len(node.Predicates) == 0 {
			r.plan.Eliminate(node)
			changed = true
		}
	}
	return changed
}

var _ rule = (*removeNoopFilter)(nil)

// predicatePushdown is a rule that moves down filter predicates to the scan nodes.
type predicatePushdown struct {
	plan *physicalpb.Plan
}

// apply implements rule.
func (r *predicatePushdown) apply(node physicalpb.Node) bool {
	changed := false
	switch node := node.(type) {
	case *physicalpb.Filter:
		for i := 0; i < len(node.Predicates); i++ {
			if ok := r.applyPredicatePushdown(node, *node.Predicates[i]); ok {
				changed = true
				// remove predicates that have been pushed down
				node.Predicates = slices.Delete(node.Predicates, i, i+1)
				i--
			}
		}
	}
	return changed
}

func (r *predicatePushdown) applyPredicatePushdown(node physicalpb.Node, predicate physicalpb.Expression) bool {
	switch node := node.(type) {
	case *physicalpb.ScanSet:
		if canApplyPredicate(predicate) {
			node.Predicates = append(node.Predicates, &predicate)
			return true
		}
		return false
	case *physicalpb.DataObjScan:
		if canApplyPredicate(predicate) {
			node.Predicates = append(node.Predicates, &predicate)
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

func canApplyPredicate(predicate physicalpb.Expression) bool {
	switch pr := predicate.Kind.(type) {
	case *physicalpb.Expression_BinaryExpression:
		return canApplyPredicate(*pr.BinaryExpression.Left) && canApplyPredicate(*pr.BinaryExpression.Right)
	case *physicalpb.Expression_ColumnExpression:
		return (pr.ColumnExpression.Type == physicalpb.COLUMN_TYPE_BUILTIN) || (pr.ColumnExpression.Type == physicalpb.COLUMN_TYPE_METADATA)
	case *physicalpb.Expression_LiteralExpression:
		return true
	default:
		return false
	}
}

var _ rule = (*predicatePushdown)(nil)

// limitPushdown is a rule that moves down the limit to the scan nodes.
type limitPushdown struct {
	plan *physicalpb.Plan
}

// apply implements rule.
func (r *limitPushdown) apply(node physicalpb.Node) bool {
	switch node.Kind() {
	case physicalpb.NodeKindLimit:
		return r.applyLimitPushdown(node, node.ToPlanNode().GetLimit().Fetch)
	}
	return false
}

func (r *limitPushdown) applyLimitPushdown(node physicalpb.Node, limit uint32) bool {
	var changed bool
	n := node.ToPlanNode()
	switch node.Kind() {
	case physicalpb.NodeKindTopK:
		n.GetTopK().K = max(n.GetTopK().K, int64(limit))
		changed = true
	case physicalpb.NodeKindDataObjScan:
		// In case the scan node is reachable from multiple different limit nodes, we need to take the largest limit.
		n.GetScan().Limit = max(n.GetScan().Limit, limit)
		return true
	case physicalpb.NodeKindFilter:
		// If there is a filter, child nodes may need to read up to all their lines to successfully apply the filter, so stop applying limit pushdown.
		return false
	}

	for _, child := range r.plan.Children(node) {
		if ok := r.applyLimitPushdown(child, limit); ok {
			changed = true
		}
	}
	return changed
}

var _ rule = (*limitPushdown)(nil)

// projectionPushdown is a rule that pushes down column projections.
// Currently, it only projects partition labels from range aggregations to scan nodes.
type projectionPushdown struct {
	plan *physicalpb.Plan
}

// apply implements rule.
func (r *projectionPushdown) apply(node physicalpb.Node) bool {
	n := node.ToPlanNode()
	switch node.Kind() {
	case physicalpb.NodeKindAggregateVector:
		if len(n.GetAggregateVector().GroupBy) == 0 {
			return false
		}

		// Pushdown from vector aggregation to range aggregations is only valid for:
		// SUM -> COUNT
		// SUM -> SUM
		// MAX -> MAX
		// MIN -> MIN

		applyToRangeAggregations := func(ops ...physicalpb.AggregateRangeOp) bool {
			anyChanged := false
			for _, child := range r.plan.Children(node) {
				if child.Kind() == physicalpb.NodeKindAggregateRange {
					ra := child.ToPlanNode().GetAggregateRange()
					if slices.Contains(ops, ra.Operation) {
						anyChanged = r.handleRangeAggregation(ra, n.GetAggregateVector().GroupBy) || anyChanged
					}
				}
			}
			return anyChanged
		}

		switch node.ToPlanNode().GetAggregateVector().Operation {
		case physicalpb.AGGREGATE_VECTOR_OP_SUM:
			return applyToRangeAggregations(physicalpb.AGGREGATE_RANGE_OP_SUM, physicalpb.AGGREGATE_RANGE_OP_COUNT)
		case physicalpb.AGGREGATE_VECTOR_OP_MAX:
			return applyToRangeAggregations(physicalpb.AGGREGATE_RANGE_OP_MAX)
		case physicalpb.AGGREGATE_VECTOR_OP_MIN:
			return applyToRangeAggregations(physicalpb.AGGREGATE_RANGE_OP_MIN)
		default:
			return false
		}
	case physicalpb.NodeKindAggregateRange:
		if !slices.Contains(physicalpb.SupportedRangeAggregationTypes, n.GetAggregateRange().Operation) {
			return false
		}
		ar := n.GetAggregateRange()
		projections := make([]*physicalpb.ColumnExpression, len(ar.PartitionBy)+1)
		copy(projections, ar.PartitionBy)
		// Always project timestamp column even if partitionBy is empty.
		// Timestamp values are required to perform range aggregation.
		projections[len(ar.PartitionBy)] = &physicalpb.ColumnExpression{Name: types.ColumnNameBuiltinTimestamp, Type: physicalpb.COLUMN_TYPE_BUILTIN}

		return r.pushToChildren(node, projections, false)
	case physicalpb.NodeKindFilter:
		projections := extractColumnsFromPredicates(node.ToPlanNode().GetFilter().Predicates)
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
	node physicalpb.Node,
	projections []*physicalpb.ColumnExpression,
	applyIfNotEmpty bool,
) bool {
	switch node.Kind() {
	case physicalpb.NodeKindScanSet:
		return r.handleScanSet(node.ToPlanNode().GetScanSet(), projections, applyIfNotEmpty)
	case physicalpb.NodeKindDataObjScan:
		return r.handleDataObjScan(node.ToPlanNode().GetScan(), projections, applyIfNotEmpty)
	case physicalpb.NodeKindParse:
		return r.handleParseNode(node.ToPlanNode().GetParse(), projections, applyIfNotEmpty)
	case physicalpb.NodeKindProjection:
		return r.handleProjection(node.ToPlanNode().GetProjection(), projections)
	case physicalpb.NodeKindAggregateRange:
		return r.handleRangeAggregation(node.ToPlanNode().GetAggregateRange(), projections)
	case physicalpb.NodeKindFilter, physicalpb.NodeKindColumnCompat, physicalpb.NodeKindParallelize:
		// Push to next direct child that cares about projections
		return r.pushToChildren(node, projections, applyIfNotEmpty)
	}

	return false
}

// handleScanSet handles projection pushdown for ScanSet nodes
func (r *projectionPushdown) handleScanSet(node *physicalpb.ScanSet, projections []*physicalpb.ColumnExpression, applyIfNotEmpty bool) bool {
	shouldNotApply := len(projections) == 0 && applyIfNotEmpty
	if !r.isMetricQuery() || shouldNotApply {
		return false
	}

	// Add to scan projections if not already present
	changed := false
	for _, colExpr := range projections {
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

// handleDataObjScan handles projection pushdown for DataObjScan nodes
func (r *projectionPushdown) handleDataObjScan(node *physicalpb.DataObjScan, projections []*physicalpb.ColumnExpression, applyIfNotEmpty bool) bool {
	shouldNotApply := len(projections) == 0 && applyIfNotEmpty
	if !r.isMetricQuery() || shouldNotApply {
		return false
	}

	// Add to scan projections if not already present
	changed := false
	for _, colExpr := range projections {
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
func (r *projectionPushdown) handleParseNode(node *physicalpb.Parse, projections []*physicalpb.ColumnExpression, applyIfNotEmpty bool) bool {
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
		// Only collect ambiguous columns to push to parse nodes
		if !requestedKeys[p.Name] {
			requestedKeys[p.Name] = true
		}
	}

	changed := len(requestedKeys) > len(node.RequestedKeys)
	if changed {
		// Convert back to sorted slice
		newKeys := slices.Collect(maps.Keys(requestedKeys))
		sort.Strings(newKeys)
		node.RequestedKeys = newKeys
	}

	projectionsToPushDown := make([]*physicalpb.ColumnExpression, len(unambiguousProjections)+1)
	copy(projectionsToPushDown, unambiguousProjections)
	projectionsToPushDown[len(projectionsToPushDown)-1] = &physicalpb.ColumnExpression{Name: types.ColumnNameBuiltinMessage, Type: physicalpb.COLUMN_TYPE_BUILTIN}

	// Push non-ambiguous projections down to children that care about them
	childrenChanged := r.pushToChildren(node, projectionsToPushDown, true)
	return changed || childrenChanged
}

// handleRangeAggregation handles projection pushdown for RangeAggregation nodes
func (r *projectionPushdown) handleRangeAggregation(node *physicalpb.AggregateRange, projections []*physicalpb.ColumnExpression) bool {
	changed := false
	for _, colExpr := range projections {
		var wasAdded bool
		node.PartitionBy, wasAdded = addUniqueProjection(node.PartitionBy, colExpr)
		if wasAdded {
			changed = true
		}
	}
	return changed
}

// handleRangeAggregation handles projection pushdown for Projection nodes
func (r *projectionPushdown) handleProjection(node *physicalpb.Projection, projections []*physicalpb.ColumnExpression) bool {
	changed := false
	if node.Expand {
		for _, e := range node.Expressions {
			switch e.Kind.(type) {
			case *physicalpb.Expression_UnaryExpression:
				if slices.Contains([]physicalpb.UnaryOp{physicalpb.UNARY_OP_CAST_FLOAT, physicalpb.UNARY_OP_CAST_BYTES, physicalpb.UNARY_OP_CAST_DURATION}, e.GetUnaryExpression().Op) {
					if e.GetUnaryExpression().Value.GetColumnExpression() != nil {
						projections = append(projections, e.GetUnaryExpression().Value.GetColumnExpression())
						changed = true
					}
				}
			}
		}
	}
	return changed
}

// pushToChildren is a helper method to push projections to all children of a node
func (r *projectionPushdown) pushToChildren(node physicalpb.Node, projections []*physicalpb.ColumnExpression, applyIfNotEmpty bool) bool {
	var anyChanged bool
	for _, child := range r.plan.Children(node) {
		if changed := r.applyProjectionPushdown(child, projections, applyIfNotEmpty); changed {
			anyChanged = true
		}
	}
	return anyChanged
}

func sortProjections(a, b *physicalpb.ColumnExpression) int {
	if a.Name < b.Name {
		return -1
	}
	if a.Name > b.Name {
		return 1
	}

	return 0
}

// isMetricQuery checks if the plan contains a RangeAggregation or VectorAggregation node, indicating a metric query
func (r *projectionPushdown) isMetricQuery() bool {
	for _, node := range r.plan.Nodes {
		switch node.Kind.(type) {
		case *physicalpb.PlanNode_AggregateRange, *physicalpb.PlanNode_AggregateVector:
			return true
		}
	}
	return false
}

var _ rule = (*projectionPushdown)(nil)

// parallelPushdown is a rule that moves or splits supported operations as a
// child of [Parallelize] to parallelize as much work as possible.
type parallelPushdown struct {
	plan   *physicalpb.Plan
	pushed map[physicalpb.Node]struct{}
}

var _ rule = (*parallelPushdown)(nil)

func (p *parallelPushdown) apply(node physicalpb.Node) bool {
	// canPushdown only returns true if all children of node are [Parallelize].
	if !p.canPushdown(node) {
		return false
	}

	if p.pushed == nil {
		p.pushed = make(map[physicalpb.Node]struct{})
	} else if _, ok := p.pushed[node]; ok {
		// Don't apply the rule to a node more than once.
		return false
	}

	// There are two catchall cases here:
	//
	// 1. Nodes which get *shifted* down into a parallel pushdown, where the
	//    positions of the node and the Parallelize swap.
	//
	//    For example, filtering gets moved down to be parallelized.
	//
	// 2. Nodes which get *sharded* into a parallel pushdown, where a copy of
	//    the node is injected into each child of the Parallelize.
	//
	//    For example, a TopK gets copied for local TopK, which is then merged back
	//    up to the parent TopK.
	//
	// There can be additional special cases, such as parallelizing an `avg` by
	// pushing down a `sum` and `count` into the Parallelize.
	switch node.(type) {
	case *physicalpb.Projection, *physicalpb.Filter, *physicalpb.Parse: // Catchall for shifting nodes
		for _, parallelize := range p.plan.Children(node) {
			p.plan.Inject(parallelize, node.CloneWithNewID())
		}
		p.plan.Eliminate(node)
		p.pushed[node] = struct{}{}
		return true

	case *physicalpb.TopK: // Catchall for sharding nodes
		for _, parallelize := range p.plan.Children(node) {
			p.plan.Inject(parallelize, node.CloneWithNewID())
		}
		p.pushed[node] = struct{}{}
		return true
	}

	return false
}

// canPushdown returns true if the given node has children that are all of type
// [NodeTypeParallelize]. Nodes with no children are not supported.
func (p *parallelPushdown) canPushdown(node physicalpb.Node) bool {
	children := p.plan.Children(node)
	if len(children) == 0 {
		// Must have at least one child.
		return false
	}

	// foundNonParallelize is true if there is at least one child that is not of
	// type [NodeTypeParallelize].
	foundNonParallelize := slices.ContainsFunc(children, func(n physicalpb.Node) bool {
		return n.Kind() != physicalpb.NodeKindParallelize
	})
	return !foundNonParallelize
}

// disambiguateColumns splits columns into ambiguous and unambiguous columns
func disambiguateColumns(columns []*physicalpb.ColumnExpression) ([]*physicalpb.ColumnExpression, []*physicalpb.ColumnExpression) {
	ambiguousColumns := make([]*physicalpb.ColumnExpression, 0, len(columns))
	unambiguousColumns := make([]*physicalpb.ColumnExpression, 0, len(columns))
	for _, col := range columns {
		// Only collect ambiguous columns (might need parsing)
		// Skip labels (from stream selector) and builtins (like timestamp/message)
		if col.Type == physicalpb.COLUMN_TYPE_AMBIGUOUS {
			ambiguousColumns = append(ambiguousColumns, col)
		} else {
			unambiguousColumns = append(unambiguousColumns, col)
		}
	}

	return unambiguousColumns, ambiguousColumns
}

// optimization represents a single optimization pass and can hold multiple rules.
type optimization struct {
	plan  *physicalpb.Plan
	name  string
	rules []rule
}

func newOptimization(name string, plan *physicalpb.Plan) *optimization {
	return &optimization{
		name: name,
		plan: plan,
	}
}

func (o *optimization) withRules(rules ...rule) *optimization {
	o.rules = append(o.rules, rules...)
	return o
}

func (o *optimization) optimize(node physicalpb.Node) {
	iterations, maxIterations := 0, 3

	for iterations < maxIterations {
		iterations++

		if !o.applyRules(node) {
			// Stop immediately if an optimization pass produced no changes.
			break
		}
	}
}

func (o *optimization) applyRules(node physicalpb.Node) bool {
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
	plan          *physicalpb.Plan
	optimisations []*optimization
}

func newOptimizer(plan *physicalpb.Plan, passes []*optimization) *optimizer {
	return &optimizer{plan: plan, optimisations: passes}
}

func (o *optimizer) optimize(node physicalpb.Node) {
	for _, optimisation := range o.optimisations {
		optimisation.optimize(node)
	}
}

func extractColumnsFromPredicates(predicates []*physicalpb.Expression) []*physicalpb.ColumnExpression {
	columns := make([]*physicalpb.ColumnExpression, 0, len(predicates))
	for _, p := range predicates {
		extractColumnsFromExpression(*p, &columns)
	}

	return deduplicateColumns(columns)
}

func extractColumnsFromExpression(expr physicalpb.Expression, columns *[]*physicalpb.ColumnExpression) {
	switch ex := expr.Kind.(type) {
	case *physicalpb.Expression_ColumnExpression:
		*columns = append(*columns, ex.ColumnExpression)
	case *physicalpb.Expression_BinaryExpression:
		extractColumnsFromExpression(*ex.BinaryExpression.Left, columns)
		extractColumnsFromExpression(*ex.BinaryExpression.Right, columns)
	case *physicalpb.Expression_UnaryExpression:
		extractColumnsFromExpression(*ex.UnaryExpression.Value, columns)
	default:
		// Ignore other expression types
	}
}

func deduplicateColumns(columns []*physicalpb.ColumnExpression) []*physicalpb.ColumnExpression {
	seen := make(map[string]bool)
	var result []*physicalpb.ColumnExpression

	for _, col := range columns {
		if !seen[col.Name] {
			seen[col.Name] = true
			result = append(result, col)
		}
	}

	return result
}

// addUniqueProjection adds a column to the projections list if it's not already present
func addUniqueProjection(projections []*physicalpb.ColumnExpression, colExpr *physicalpb.ColumnExpression) ([]*physicalpb.ColumnExpression, bool) {
	for _, existing := range projections {
		if existing.Name == colExpr.Name {
			return projections, false // already exists
		}
	}
	return append(projections, colExpr), true
}
