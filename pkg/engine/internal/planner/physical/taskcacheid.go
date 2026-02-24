package physical

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// cacheKeyListJoin joins sorted items with "|" for use in readable cache keys.
func cacheKeyListJoin(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "|")
}

// expressionStrings returns sorted string representations of expressions for deterministic hashing.
func expressionStrings(exprs []Expression) []string {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]string, len(exprs))
	for i, e := range exprs {
		if e != nil {
			out[i] = e.String()
		}
	}
	slices.Sort(out)
	return out
}

// predicateStringForHash returns the string to use when hashing a predicate, clamping
// timestamp bounds to maxRange so that queries spanning the full data object share the same key.
// If logger is non-nil, logs when a timestamp predicate is clamped.
func predicateStringForHash(expr Expression, maxRange TimeRange, logger log.Logger) string {
	bin, ok := expr.(*BinaryExpr)
	if !ok {
		return expr.String()
	}
	col, ok := bin.Left.(*ColumnExpr)
	if !ok || col.Ref.Column != types.ColumnNameBuiltinTimestamp || col.Ref.Type != types.ColumnTypeBuiltin {
		return expr.String()
	}
	lit, ok := bin.Right.(*LiteralExpr)
	if !ok || lit.ValueType() != types.Loki.Timestamp {
		return expr.String()
	}
	ts, ok := lit.Value().(types.Timestamp)
	if !ok {
		return expr.String()
	}
	t := time.Unix(0, int64(ts))
	clamped := t
	switch bin.Op {
	case types.BinaryOpGte, types.BinaryOpGt:
		if t.Before(maxRange.Start) {
			clamped = maxRange.Start
		}
	case types.BinaryOpLt, types.BinaryOpLte:
		if t.After(maxRange.End) {
			clamped = maxRange.End
		}
	default:
		return expr.String()
	}
	if logger != nil && clamped != t {
		level.Info(logger).Log(
			"msg", "task_cache_id: clamped timestamp predicate to max_time_range",
			"op", bin.Op.String(),
			"original_ts", util.FormatTimeRFC3339Nano(t),
			"clamped_ts", util.FormatTimeRFC3339Nano(clamped),
			"original_expr", expr.String(),
			"max_time_range_start", util.FormatTimeRFC3339Nano(maxRange.Start),
			"max_time_range_end", util.FormatTimeRFC3339Nano(maxRange.End),
		)
	}
	return fmt.Sprintf("%s(%s, %s)", bin.Op, bin.Left, util.FormatTimeRFC3339Nano(clamped))
}

// dataObjScanPredicateStrings returns sorted predicate strings for hashing, with timestamp bounds clamped to maxRange.
// If logger is non-nil, logs when any predicate is clamped.
func dataObjScanPredicateStrings(predicates []Expression, maxRange TimeRange, logger log.Logger) []string {
	if len(predicates) == 0 {
		return nil
	}
	out := make([]string, len(predicates))
	for i, e := range predicates {
		if e != nil {
			out[i] = predicateStringForHash(e, maxRange, logger)
		}
	}
	slices.Sort(out)
	return out
}

// cacheKeyStringDataObjScan returns a deterministic, readable cache key string for a DataObjScan.
// If logger is non-nil, logs when any timestamp predicate is clamped to max_time_range.
func cacheKeyStringDataObjScan(s *DataObjScan, logger log.Logger) string {
	streamIDs := slices.Clone(s.StreamIDs)
	sort.Slice(streamIDs, func(i, j int) bool { return streamIDs[i] < streamIDs[j] })
	streamParts := make([]string, len(streamIDs))
	for i, id := range streamIDs {
		streamParts[i] = fmt.Sprintf("%d", id)
	}
	predStrs := dataObjScanPredicateStrings(s.Predicates, s.MaxTimeRange, logger)
	projStrs := expressionStrings(exprSliceToExpression(s.Projections))
	return fmt.Sprintf("DataObjScan location=%s section=%d streams=%s predicates=%s projections=%s max_start=%s max_end=%s",
		string(s.Location),
		s.Section,
		strings.Join(streamParts, ","),
		cacheKeyListJoin(predStrs),
		cacheKeyListJoin(projStrs),
		util.FormatTimeRFC3339Nano(s.MaxTimeRange.Start),
		util.FormatTimeRFC3339Nano(s.MaxTimeRange.End),
	)
}

// exprSliceToExpression converts []ColumnExpression to []Expression for expressionStrings.
func exprSliceToExpression(ce []ColumnExpression) []Expression {
	if len(ce) == 0 {
		return nil
	}
	out := make([]Expression, len(ce))
	for i, e := range ce {
		out[i] = e
	}
	return out
}

// cacheKeyStringPointersScan returns a deterministic, readable cache key string for a PointersScan.
// Start/End are clamped to MaxTimeRange only when the query range fully covers the index range
// (query start <= max start and query end >= max end), so that queries spanning the full index share the same key.
func cacheKeyStringPointersScan(s *PointersScan) string {
	selectorStr := ""
	if s.Selector != nil {
		selectorStr = s.Selector.String()
	}
	predStrs := expressionStrings(s.Predicates)
	idxRng := s.GetMaxTimeRange()
	queryStart, queryEnd := s.Start, s.End
	if !idxRng.IsZero() && queryStart.Before(idxRng.Start) && queryEnd.After(idxRng.End) {
		// Query fully covers the index range; use index range in key for cache reuse
		queryStart, queryEnd = idxRng.Start, idxRng.End
	}
	return fmt.Sprintf("PointersScan location=%s selector=%s start=%s end=%s predicates=%s",
		string(s.Location),
		selectorStr,
		util.FormatTimeRFC3339Nano(queryStart),
		util.FormatTimeRFC3339Nano(queryEnd),
		cacheKeyListJoin(predStrs),
	)
}

// cacheKeyStringDataObject returns a deterministic cache key for a data object section.
func cacheKeyStringDataObject(location DataObjLocation, section int) string {
	return fmt.Sprintf("DataObject location=%s section=%d", string(location), section)
}

// PlanCacheKey returns a deterministic cache key for the entire plan by
// concatenating the CacheableKey() of all nodes in DFS post-order (scan nodes
// first, root last). It also returns the dominant scan node type
// (NodeTypeDataObjScan or NodeTypePointersScan) so callers can route to the
// appropriate cache. Returns ("", NodeTypeInvalid, false) if the plan contains
// no scan nodes (e.g. outer aggregation tasks that consume from other tasks).
func PlanCacheKey(plan *Plan) (key string, scanType NodeType, ok bool) {
	var parts []string
	for _, root := range plan.Roots() {
		_ = plan.DFSWalk(root, func(n Node) error {
			switch n.Type() {
			case NodeTypeDataObjScan, NodeTypePointersScan:
				scanType = n.Type()
			}
			if k := n.CacheableKey(); k != "" {
				parts = append(parts, k)
			}
			return nil
		}, dag.PostOrderWalk)
	}
	if scanType == NodeTypeInvalid {
		return "", NodeTypeInvalid, false
	}
	return strings.Join(parts, "|"), scanType, true
}
