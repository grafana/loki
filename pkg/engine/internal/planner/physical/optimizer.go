package physical

import (
	"maps"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// A rule is a transformation that can be applied on a Node.
type rule interface {
	// apply tries to apply the transformation on the node.
	// It returns a boolean indicating whether the transformation has been applied.
	apply(Node) bool
}

var _ rule = (*removeNoopFilter)(nil)

// removeNoopFilter is a rule that removes Filter nodes without predicates.
type removeNoopFilter struct {
	plan *Plan
}

// apply implements rule.
func (r *removeNoopFilter) apply(root Node) bool {
	// collect filter nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Filter)
		return ok
	})

	changed := false
	for _, n := range nodes {
		filter := n.(*Filter)
		if len(filter.Predicates) == 0 {
			r.plan.graph.Eliminate(filter)
			changed = true
		}
	}

	return changed
}

var _ rule = (*predicatePushdown)(nil)

// predicatePushdown is a rule that moves down filter predicates to the scan nodes.
type predicatePushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *predicatePushdown) apply(root Node) bool {
	// collect filter nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Filter)
		return ok
	})

	changed := false
	for _, n := range nodes {
		filter := n.(*Filter)
		for i := 0; i < len(filter.Predicates); i++ {
			if !canApplyPredicate(filter.Predicates[i]) {
				continue
			}

			if ok := r.applyToTargets(filter, filter.Predicates[i]); ok {
				changed = true
				// remove predicates that have been pushed down
				filter.Predicates = slices.Delete(filter.Predicates, i, i+1)
				i--
			}
		}
	}

	return changed
}

func (r *predicatePushdown) applyToTargets(node Node, predicate Expression) bool {
	switch node := node.(type) {
	case *ScanSet:
		node.Predicates = append(node.Predicates, predicate)
		return true
	case *DataObjScan:
		node.Predicates = append(node.Predicates, predicate)
		return true
	}

	changed := false
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, predicate) {
			changed = true
		}
	}
	return changed
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

var _ rule = (*limitPushdown)(nil)

// limitPushdown is a rule that moves down the limit to the scan nodes.
type limitPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *limitPushdown) apply(root Node) bool {
	// collect limit nodes.
	nodes := findMatchingNodes(r.plan, root, func(node Node) bool {
		_, ok := node.(*Limit)
		return ok
	})

	// propagate limit to target child nodes.
	changed := false
	for _, n := range nodes {
		limit := n.(*Limit)
		if r.applyToTargets(limit, limit.Fetch) {
			changed = true
		}
	}
	return changed
}

// applyToTargets applies limit on target nodes.
func (r *limitPushdown) applyToTargets(node Node, limit uint32) bool {
	var changed bool
	switch node := node.(type) {
	case *TopK:
		node.K = max(node.K, int(limit))
		changed = true
	case *Filter:
		// If there is a filter, child nodes may need to read up to all their lines
		// to successfully apply the filter, so stop applying limit pushdown.
		return false
	}

	// Continue to children
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, limit) {
			changed = true
		}
	}
	return changed
}

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
		if len(vecAgg.GroupBy) == 0 {
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

		if r.applyToTargets(vecAgg, vecAgg.GroupBy, supportedAggTypes...) {
			changed = true
		}
	}

	return changed
}

func (r *groupByPushdown) applyToTargets(node Node, groupBy []ColumnExpression, supportedAggTypes ...types.RangeAggregationType) bool {
	var changed bool
	switch node := node.(type) {
	case *RangeAggregation:
		if !slices.Contains(supportedAggTypes, node.Operation) {
			return false
		}

		for _, colExpr := range groupBy {
			colExpr, ok := colExpr.(*ColumnExpr)
			if !ok {
				continue
			}

			var wasAdded bool
			node.PartitionBy, wasAdded = addUniqueColumnExpr(node.PartitionBy, colExpr)
			if wasAdded {
				changed = true
			}
		}

		return changed
	}

	// Continue to children
	for _, child := range r.plan.Children(node) {
		if r.applyToTargets(child, groupBy, supportedAggTypes...) {
			changed = true
		}
	}

	return changed
}

var _ rule = (*projectionPushdown)(nil)

// projectionPushdown is a rule that pushes down column projections.
type projectionPushdown struct {
	plan *Plan
}

// apply implements rule.
func (r *projectionPushdown) apply(node Node) bool {
	if !r.isMetricQuery() {
		return false
	}

	return r.propagateProjections(node, nil)
}

// propagateProjections propagates projections down the plan tree.
// It collects required columns from source nodes (consumers) and pushes them down to target nodes (scanners).
func (r *projectionPushdown) propagateProjections(node Node, projections []ColumnExpression) bool {
	var changed bool
	switch node := node.(type) {
	case *RangeAggregation:
		// [Source] RangeAggregation requires partitionBy columns & timestamp.
		projections = append(projections, node.PartitionBy...)
		// Always project timestamp column even if partitionBy is empty.
		// Timestamp values are required to perform range aggregation.
		projections = append(projections, &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}})
	case *Filter:
		// [Source] Filter nodes require predicate columns.
		extracted := extractColumnsFromPredicates(node.Predicates)
		projections = append(projections, extracted...)

	case *ParseNode:
		// ParseNode is a special case. It is both a target for projections and a source of projections.
		// [Target] Ambiguous columns are applied as requested keys to ParseNode.
		// [Source] Appends builtin message column.
		var parseNodeChanged bool
		parseNodeChanged, projections = r.handleParseNode(node, projections)
		if parseNodeChanged {
			changed = true
		}

	case *ScanSet:
		// [Target] ScanSet - projections are applied here.
		return r.handleScanSet(node, projections)

	case *DataObjScan:
		// [Target] DataObjScan - projections are applied here.
		return r.handleDataobjScan(node, projections)

	case *Projection:
		if node.Expand {
			// [Source] column referred by unwrap.
			for _, e := range node.Expressions {
				e, isUnary := e.(*UnaryExpr)
				if isUnary && slices.Contains([]types.UnaryOp{types.UnaryOpCastFloat, types.UnaryOpCastBytes, types.UnaryOpCastDuration}, e.Op) {
					projections = append(projections, e.Left.(ColumnExpression))
				}
			}
		}
	default:
		// propagate to children
	}

	// dedupe after updating projection list
	deduplicateColumns(projections)

	// Continue to children
	for _, child := range r.plan.Children(node) {
		if r.propagateProjections(child, projections) {
			changed = true
		}
	}

	return changed
}

// handleScanSet handles projection pushdown for ScanSet nodes
func (r *projectionPushdown) handleScanSet(node *ScanSet, projections []ColumnExpression) bool {
	if len(projections) == 0 {
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
		node.Projections, wasAdded = addUniqueColumnExpr(node.Projections, colExpr)
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

// handleDataobjScan handles projection pushdown for DataObjScan nodes
func (r *projectionPushdown) handleDataobjScan(node *DataObjScan, projections []ColumnExpression) bool {
	if len(projections) == 0 {
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
		node.Projections, wasAdded = addUniqueColumnExpr(node.Projections, colExpr)
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
func (r *projectionPushdown) handleParseNode(node *ParseNode, projections []ColumnExpression) (bool, []ColumnExpression) {
	_, ambiguousProjections := disambiguateColumns(projections)

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

	projections = append(projections, &ColumnExpr{
		Ref: types.ColumnRef{Column: types.ColumnNameBuiltinMessage, Type: types.ColumnTypeBuiltin},
	})

	return changed, projections
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
	for node := range r.plan.graph.Nodes() {
		if _, ok := node.(*RangeAggregation); ok {
			return true
		}
		if _, ok := node.(*VectorAggregation); ok {
			return true
		}
	}
	return false
}

// parallelPushdown is a rule that moves or splits supported operations as a
// child of [Parallelize] to parallelize as much work as possible.
type parallelPushdown struct {
	plan   *Plan
	pushed map[Node]struct{}
}

var _ rule = (*parallelPushdown)(nil)

func (p *parallelPushdown) apply(root Node) bool {
	if p.pushed == nil {
		p.pushed = make(map[Node]struct{})
	}

	// find all nodes that can be parallelized
	nodes := findMatchingNodes(p.plan, root, func(node Node) bool {
		if _, ok := p.pushed[node]; ok {
			return false
		}

		// canPushdown only returns true if all children of node are [Parallelize].
		return p.canPushdown(node)
	})

	// apply parallel pushdown to each node
	changed := false
	for _, node := range nodes {
		if p.applyParallelization(node) {
			changed = true
		}
	}

	return changed
}

func (p *parallelPushdown) applyParallelization(node Node) bool {
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
	case *Projection, *Filter, *ParseNode, *ColumnCompat: // Catchall for shifting nodes
		for _, parallelize := range p.plan.Children(node) {
			p.plan.graph.Inject(parallelize, node.Clone())
		}
		p.plan.graph.Eliminate(node)
		p.pushed[node] = struct{}{}
		return true

	case *TopK: // Catchall for sharding nodes
		// TODO: Add Range aggregation as a sharding node

		for _, parallelize := range p.plan.Children(node) {
			p.plan.graph.Inject(parallelize, node.Clone())
		}
		p.pushed[node] = struct{}{}
		return true
	}

	return false
}

// canPushdown returns true if the given node has children that are all of type
// [NodeTypeParallelize]. Nodes with no children are not supported.
func (p *parallelPushdown) canPushdown(node Node) bool {
	children := p.plan.Children(node)
	if len(children) == 0 {
		// Must have at least one child.
		return false
	}

	// foundNonParallelize is true if there is at least one child that is not of
	// type [NodeTypeParallelize].
	foundNonParallelize := slices.ContainsFunc(children, func(n Node) bool {
		return n.Type() != NodeTypeParallelize
	})
	return !foundNonParallelize
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
	iterations, maxIterations := 0, 10

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

	for _, rule := range o.rules {
		if rule.apply(node) {
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

// addUniqueColumnExpr adds a column to the projections list if it's not already present
func addUniqueColumnExpr(projections []ColumnExpression, colExpr *ColumnExpr) ([]ColumnExpression, bool) {
	for _, existing := range projections {
		if existingCol, ok := existing.(*ColumnExpr); ok {
			if existingCol.Ref.Column == colExpr.Ref.Column {
				return projections, false // already exists
			}
		}
	}
	return append(projections, colExpr), true
}

// findMatchingNodes finds all nodes in the plan tree that match the given matchFn.
func findMatchingNodes(plan *Plan, root Node, matchFn func(Node) bool) []Node {
	var result []Node
	// Using PostOrderWalk to return child nodes first.
	// This can be useful for optimizations like predicate pushdown
	// where it is ideal to process child Filter before parent Filter.
	_ = plan.graph.Walk(root, func(node Node) error {
		if matchFn(node) {
			result = append(result, node)
		}
		return nil
	}, dag.PostOrderWalk)
	return result
}
