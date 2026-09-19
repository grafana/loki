package physical

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
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
				switch e.Op {
				case types.VariadicOpParseJSON, types.VariadicOpParseLogfmt:
					projectionNodeChanged, projsToPropagate := r.handleParse(e, projections)
					projections = append(projections, projsToPropagate...)
					if projectionNodeChanged {
						changed = true
					}
				case types.VariadicOpParseRegexp:
					projections = append(projections, r.handleParseRegexp(e)...)
				case types.VariadicOpParseLinefmt:
					projections = append(projections, r.handleParseLinefmt(e)...)
				case types.VariadicOpParseLabelfmt:
					projections = append(projections, r.handleParseLabelfmt(e)...)
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

	// Source columns of collision-renamed "_extracted" references that must be
	// projected from the scan (in addition to parsed) so the rename can happen.
	var collisionSources []ColumnExpression

	for _, p := range ambiguousProjections {
		colExpr, ok := p.(*ColumnExpr)
		if !ok {
			continue
		}

		// Only collect ambiguous columns to push to parse nodes
		requestedKeys[colExpr.Ref.Column] = true

		// A parsed field colliding with a higher-priority column (a stream label or
		// structured metadata) is referenced downstream with the ExtractedSuffix
		// appended ("level_extracted"), but the colliding column and the parser's source
		// key are the de-suffixed name ("level").
		// ColumnCompat re-applies the suffix to the parsed value at runtime.
		// To reproduce that we must
		// (1) request the source key from the parser, else it extracts a key that doesn't exist
		// (2) project the source column so the colliding label/metadata is actually loaded from the
		// scan, else there is nothing to collide with and the rename never happens.
		// Either gap makes a group-by/filter on the "_extracted" name drop to null. Mirrors v1,
		// where parsing happens against the full label set (see JSONParser.buildJSONPathFromPrefixBuffer).
		// Both names are kept in case a real "<x>_extracted" field exists.
		if src, found := strings.CutSuffix(colExpr.Ref.Column, semconv.ExtractedSuffix); found && src != "" {
			requestedKeys[src] = true
			collisionSources = append(collisionSources, &ColumnExpr{Ref: types.ColumnRef{Column: src, Type: types.ColumnTypeAmbiguous}})
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
	projections = append(projections, collisionSources...)
	return changed, projections
}

// handleParseLinefmt returns the fields referenced by the line_format
// template as Ambiguous column expressions, so an upstream parser
// (logfmt / json) extracts them into the batch before line_format
// consumes them. Without this, template lookups like {{.mint}}
// silently render "" via missingkey=zero — breaking any subsequent
// regexp/filter that needs the templated content.
func (r *projectionPushdown) handleParseLinefmt(expr *VariadicExpr) []ColumnExpression {
	if len(expr.Expressions) < 3 {
		return nil
	}
	tmplLit, ok := expr.Expressions[2].(*LiteralExpr)
	if !ok {
		return nil
	}
	tmplStr, ok := tmplLit.Literal().Any().(string)
	if !ok || tmplStr == "" {
		return nil
	}
	formatter, err := log.NewFormatter(tmplStr)
	if err != nil {
		return nil
	}
	names := formatter.RequiredLabelNames()
	if len(names) == 0 {
		return nil
	}
	out := make([]ColumnExpression, 0, len(names))
	for _, name := range names {
		out = append(out, &ColumnExpr{Ref: types.ColumnRef{Column: name, Type: types.ColumnTypeAmbiguous}})
	}
	return out
}

// handleParseLabelfmt returns the columns referenced by label_format
// stages as Ambiguous column expressions, so an upstream parser
// (logfmt / json) extracts them into the batch before label_format
// consumes them. Two forms feed into RequiredLabelNames():
//   - rename: `label_format new=old` requires the source column `old`
//     so the rename target has a value (the parser only sees real keys).
//   - template: `label_format tag="{{.foo}}"` requires each field
//     referenced by the Go template; missing ones silently render ""
//     via missingkey=zero.
func (r *projectionPushdown) handleParseLabelfmt(expr *VariadicExpr) []ColumnExpression {
	if len(expr.Expressions) < 3 {
		return nil
	}
	fmtLit, ok := expr.Expressions[2].(*LiteralExpr)
	if !ok {
		return nil
	}
	fmts, ok := fmtLit.Literal().Any().([]log.LabelFmt)
	if !ok || len(fmts) == 0 {
		return nil
	}
	formatter, err := log.NewLabelsFormatter(fmts)
	if err != nil {
		return nil
	}
	names := formatter.RequiredLabelNames()
	if len(names) == 0 {
		return nil
	}
	out := make([]ColumnExpression, 0, len(names))
	for _, name := range names {
		out = append(out, &ColumnExpr{Ref: types.ColumnRef{Column: name, Type: types.ColumnTypeAmbiguous}})
	}
	return out
}

// handleParseRegexp returns the regexp parser's source column so the scan
// loads the line content the pattern is applied against.
func (r *projectionPushdown) handleParseRegexp(expr *VariadicExpr) []ColumnExpression {
	if len(expr.Expressions) < 1 {
		return nil
	}
	sourceCol, ok := expr.Expressions[0].(*ColumnExpr)
	if !ok {
		return nil
	}
	return []ColumnExpression{sourceCol}
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

// deduplicateColumns removes duplicate column expressions from the list,
// keying by (name, type) rather than name alone. See [addUniqueColumnExpr]
// for why type matters at this layer.
func (r *projectionPushdown) deduplicateColumns(columns []ColumnExpression) []ColumnExpression {
	seen := make(map[types.ColumnRef]bool)
	var result []ColumnExpression

	for _, col := range columns {
		if colExpr, ok := col.(*ColumnExpr); ok {
			if !seen[colExpr.Ref] {
				seen[colExpr.Ref] = true
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
