package physical

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

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
		if node.Grouping.Without {
			return changed
		}
		// [Source] RangeAggregation requires partitionBy columns & timestamp.
		projections = append(projections, node.Grouping.Columns...)
		// Always project timestamp column. Timestamp values are required to perform range aggregation.
		projections = append(projections, &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}})
	case *Filter:
		// [Source] Filter nodes require predicate columns.
		extracted := r.extractColumnsFromPredicates(node.Predicates)
		projections = append(projections, extracted...)

	case *ScanSet:
		// [Target] ScanSet - projections are applied here.
		return r.handleScanSet(node, projections)

	case *DataObjScan:
		// [Target] DataObjScan - projections are applied here.
		return r.handleDataobjScan(node, projections)

	case *Projection:
		// Projections are a special case. It is both a target for and a source of projections.
		// [Target] Operations may take columns as arguments, such as requested keys for parse..
		// [Source] Operations may contain columns to append, such as builtin message column for parse or source column for unwrap.
		for _, e := range node.Expressions {
			switch e := e.(type) {
			case *UnaryExpr:
				if slices.Contains([]types.UnaryOp{types.UnaryOpCastFloat, types.UnaryOpCastBytes, types.UnaryOpCastDuration}, e.Op) {
					projections = append(projections, e.Left.(ColumnExpression))
				}
			case *VariadicExpr:
				if e.Op == types.VariadicOpParseJSON || e.Op == types.VariadicOpParseLogfmt {
					projectionNodeChanged, projsToPropagate := r.handleParse(e, projections)
					projections = append(projections, projsToPropagate...)
					if projectionNodeChanged {
						changed = true
					}
				}
			}
		}
	default:
		// propagate to children
	}

	// dedupe after updating projection list
	r.deduplicateColumns(projections)

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

		// There are no generated columns in data objects
		if colExpr.Ref.Type == types.ColumnTypeGenerated {
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

		// There are no generated columns in data objects
		if colExpr.Ref.Type == types.ColumnTypeGenerated {
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

func (r *projectionPushdown) handleParse(expr *VariadicExpr, projections []ColumnExpression) (bool, []ColumnExpression) {
	_, ambiguousProjections := r.disambiguateColumns(projections)

	var exprs parseExprs
	if err := exprs.Unpack(expr.Expressions); err != nil {
		panic(err)
	}

	requestedKeys := make(map[string]bool)

	// Handle both null and string list literals for requested keys
	switch keys := exprs.requestedKeysExpr.Literal().(type) {
	case types.StringListLiteral:
		for _, k := range keys {
			requestedKeys[k] = true
		}
	case types.NullLiteral:
		// Start with empty set
	default:
		panic(fmt.Errorf("expected requested keys to be a list of strings or null, got %T", exprs.requestedKeysExpr.Literal))
	}

	initialKeyCount := len(requestedKeys)

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

	changed := len(requestedKeys) > initialKeyCount
	if changed {
		// Convert back to sorted slice
		newKeys := slices.Collect(maps.Keys(requestedKeys))
		sort.Strings(newKeys)
		exprs.requestedKeysExpr = NewLiteral(newKeys)
	}

	expr.Expressions = exprs.Pack(expr.Expressions)
	projections = append(projections, exprs.sourceColumnExpr)
	return changed, projections
}

// parseExprs is a helper struct for unpacking and packing parse arguments from generic expressions.
type parseExprs struct {
	sourceColumnExpr  *ColumnExpr
	requestedKeysExpr *LiteralExpr
	strictExpr        *LiteralExpr
	keepEmptyExpr     *LiteralExpr
}

// Unpack unpacks the given expressions into valid expressions for parse.
// Valid expressions for parse are ones that will evaluate into valid arguments for a [parseFn].
// The valid signatures for a [parseFn] are:
// parseFn(sourceCol [arrow.Array], requestedKeys [arrow.Array], strict [arrow.Array], keepEmpty [arrow.Array]).
//
// Therefore the valid exprssions are (order matters):
// [sourceColExpr *ColumnExpr, requestedKeysExpr *LiteralExpr, strictExpr *LiteralExpr, keepEmptyExpr *LiteralExpr] -> parseFn(sourceColVec arrow.Array, requestedKeys arrow.Array, strict arrow.Array, keepEmpty arrow.Array)
func (a *parseExprs) Unpack(exprs []Expression) error {
	if len(exprs) != 4 {
		return fmt.Errorf("expected to unpack 4 expressions, got %d", len(exprs))
	}

	var ok bool
	a.sourceColumnExpr, ok = exprs[0].(*ColumnExpr)
	if !ok {
		return fmt.Errorf("expected source column to be a column expression, got %T", exprs[0])
	}

	a.requestedKeysExpr, ok = exprs[1].(*LiteralExpr)
	if !ok {
		return fmt.Errorf("expected requested keys to be a literal expression, got %T", exprs[1])
	}
	a.strictExpr, ok = exprs[2].(*LiteralExpr)
	if !ok {
		return fmt.Errorf("expected strict to be a literal expression, got %T", exprs[2])
	}
	a.keepEmptyExpr, ok = exprs[3].(*LiteralExpr)
	if !ok {
		return fmt.Errorf("expected keepEmpty to be a literal expression, got %T", exprs[3])
	}

	return nil
}

// Pack packs parse specific expressions back into generic expressions.
// It will resues [dst] if has enough capacity, otherwise it will allocate a new slice.
func (a *parseExprs) Pack(dst []Expression) []Expression {
	if cap(dst) >= 4 {
		dst = dst[:4]
		clear(dst[4:])
	} else {
		dst = make([]Expression, 4)
	}

	// order matters
	dst[0] = a.sourceColumnExpr
	dst[1] = a.requestedKeysExpr
	dst[2] = a.strictExpr
	dst[3] = a.keepEmptyExpr
	return dst
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

func (r *projectionPushdown) deduplicateColumns(columns []ColumnExpression) []ColumnExpression {
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

// disambiguateColumns splits columns into ambiguous and unambiguous columns
func (r *projectionPushdown) disambiguateColumns(columns []ColumnExpression) ([]ColumnExpression, []ColumnExpression) {
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

func (r *projectionPushdown) extractColumnsFromExpression(expr Expression, columns *[]ColumnExpression) {
	switch e := expr.(type) {
	case *ColumnExpr:
		*columns = append(*columns, e)
	case *BinaryExpr:
		r.extractColumnsFromExpression(e.Left, columns)
		r.extractColumnsFromExpression(e.Right, columns)
	case *UnaryExpr:
		r.extractColumnsFromExpression(e.Left, columns)
	default:
		// Ignore other expression types
	}
}

func (r *projectionPushdown) extractColumnsFromPredicates(predicates []Expression) []ColumnExpression {
	columns := make([]ColumnExpression, 0, len(predicates))
	for _, p := range predicates {
		r.extractColumnsFromExpression(p, &columns)
	}

	return r.deduplicateColumns(columns)
}
