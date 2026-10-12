package queryrange

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

// dataObjCoverageThreshold is the share of the read window that data objects must exceed. A query
// above it uses the data-object logic for time splitting and query sharding.
const dataObjCoverageThreshold = 0.5

// dataObjResolution is the result of the data-object checks for one expression.
type dataObjResolution struct {
	// outcome is dataObjOutcomeEligible, or the first check that failed.
	outcome string
	// coverage is the share of the read window that the held range covers. It is set only when
	// outcome is dataObjOutcomeEligible.
	coverage float64
}

// prefers reports whether the expression is eligible and the share of the read window that the held
// range covers is above dataObjCoverageThreshold.
func (r dataObjResolution) prefers() bool {
	return r.outcome == dataObjOutcomeEligible && r.coverage > dataObjCoverageThreshold
}

// dataObjResolve decides whether a querier may read the expression from data objects, for a
// request over the time range from start to end.
//
// The checks mirror the ones the querier makes. The config must turn data objects on. The expression
// must be a stream-first sum. Every tenant must turn stream-first and data objects on. The read
// window must overlap the held range.
//
// The read window of a query is the time range the query reads. It is wider than the request,
// because it includes the range vector and moves back by the offset. The held range is the time
// range that data objects hold. It runs from the latest start time of the tenants to the current
// time minus the storage lag.
func dataObjResolve(cfg Config, limits Limits, tenantIDs []string, expr syntax.Expr, start, end time.Time) dataObjResolution {
	if !cfg.DataObjEnabled {
		return dataObjResolution{outcome: dataObjOutcomeDataObjectsDisabled}
	}

	rng, ok := logql.StreamFirstRangeAggregationExpr(expr)
	if !ok {
		return dataObjResolution{outcome: dataObjOutcomeUnsupportedQuery}
	}

	return dataObjResolveWindow(cfg, limits, tenantIDs, rng.Left.Interval, rng.Left.Offset, start, end)
}

// dataObjAnyPrefers predicts, for a query that the frontend has not rewritten yet, whether a piece
// of it will prefer data objects.
//
// It reports true when a sub-expression has the stream-first shape, such as one leg of a binary
// expression. It then checks the read window of the longest range vector and the longest offset of
// the whole query against the held range.
//
// The frontend splits a query in time before it shards it, so it cannot yet see the pieces that a
// querier will run. A wrong prediction only changes how the frontend splits the query.
func dataObjAnyPrefers(cfg Config, limits Limits, tenantIDs []string, expr syntax.Expr, start, end time.Time) bool {
	if !cfg.DataObjEnabled {
		return false
	}

	var hasStreamFirstShape bool
	expr.Walk(func(e syntax.Expr) bool {
		if _, ok := logql.StreamFirstRangeAggregationExpr(e); ok {
			hasStreamFirstShape = true
		}
		return !hasStreamFirstShape
	})
	if !hasStreamFirstShape {
		return false
	}

	maxRange, maxOffset := maxRangeVectorAndOffsetDuration(expr)
	return dataObjResolveWindow(cfg, limits, tenantIDs, maxRange, maxOffset, start, end).prefers()
}

// dataObjResolveWindow runs the checks that depend on the tenants and the time range, for a query
// that reads a range vector of the given length at the given offset.
func dataObjResolveWindow(cfg Config, limits Limits, tenantIDs []string, interval, offset time.Duration, start, end time.Time) dataObjResolution {
	if len(tenantIDs) == 0 {
		return dataObjResolution{outcome: dataObjOutcomeDataObjectsDisabled}
	}

	for _, id := range tenantIDs {
		if !limits.StreamFirstExecutionEnabled(id) {
			return dataObjResolution{outcome: dataObjOutcomeStreamFirstDisabled}
		}
	}

	// The querier reads [start - interval - offset, end - offset]. The evaluator adds one
	// nanosecond to the end, because range iterators include the end.
	windowStart := start.Add(-interval - offset)
	windowEnd := end.Add(-offset).Add(time.Nanosecond)

	// Every tenant must read data objects. The held range of all of them starts at the latest start.
	var heldStart time.Time
	for _, id := range tenantIDs {
		tenantStart := limits.DataObjQueryStartTime(id)
		if tenantStart.IsZero() {
			return dataObjResolution{outcome: dataObjOutcomeDataObjectsDisabled}
		}
		if tenantStart.After(heldStart) {
			heldStart = tenantStart
		}
	}

	now := time.Now
	if cfg.now != nil {
		now = cfg.now
	}
	heldEnd := now().Add(-cfg.DataObjStorageLag)

	// The querier reads data objects when the read window overlaps the held range, so an empty
	// overlap makes the query ineligible. A non-empty overlap means the read window is not empty.
	overlapStart, overlapEnd := maxTime(windowStart, heldStart), minTime(windowEnd, heldEnd)
	if !overlapStart.Before(overlapEnd) {
		return dataObjResolution{outcome: dataObjOutcomeOutsideDataObjRange}
	}

	return dataObjResolution{
		outcome:  dataObjOutcomeEligible,
		coverage: float64(overlapEnd.Sub(overlapStart)) / float64(windowEnd.Sub(windowStart)),
	}
}

// planExpr returns the parsed expression of a request. It parses the query when the request has no
// plan.
func planExpr(queryPlan *plan.QueryPlan, query string) (syntax.Expr, error) {
	if queryPlan != nil && queryPlan.AST != nil {
		return queryPlan.AST, nil
	}
	return syntax.ParseExpr(query)
}
