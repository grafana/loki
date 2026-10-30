package queryrange

import (
	"time"

	"github.com/grafana/loki/v4/pkg/logql"
	"github.com/grafana/loki/v4/pkg/logql/syntax"
	"github.com/grafana/loki/v4/pkg/querier/plan"
)

// DataObjCoverageThreshold is the share of the read window that data objects must exceed. A query
// above it uses the data-object logic for time splitting and query sharding.
const DataObjCoverageThreshold = 0.5

// DataObjLimits is the part of the tenant limits that the data-object checks read.
type DataObjLimits interface {
	// StreamFirstExecutionEnabled reports whether eligible metric queries of the tenant run in
	// stream-first sample order.
	StreamFirstExecutionEnabled(userID string) bool
	// DataObjQueryStartTime returns the time from which the tenant reads stream-first metric queries
	// from data objects. The zero time turns data-object reads off for the tenant.
	DataObjQueryStartTime(userID string) time.Time
}

// DataObjResolutionOutcome is the result of the data-object checks for one expression. It is
// DataObjOutcomeEligible, or the first check that failed. The constants form a small fixed set, so
// the values are safe as metric labels.
type DataObjResolutionOutcome string

const (
	// DataObjOutcomeEligible means the expression passed every check.
	DataObjOutcomeEligible DataObjResolutionOutcome = "eligible"
	// DataObjOutcomeUnsupportedQuery means the expression is not a stream-first sum.
	DataObjOutcomeUnsupportedQuery DataObjResolutionOutcome = "unsupported_query"
	// DataObjOutcomeStreamFirstDisabled means a tenant has stream-first off.
	DataObjOutcomeStreamFirstDisabled DataObjResolutionOutcome = "stream_first_disabled"
	// DataObjOutcomeDataObjectsDisabled means data objects are off, or a tenant has no data-object
	// start time.
	DataObjOutcomeDataObjectsDisabled DataObjResolutionOutcome = "data_objects_disabled"
	// DataObjOutcomeOutsideDataObjRange means the read window does not overlap the held range.
	DataObjOutcomeOutsideDataObjRange DataObjResolutionOutcome = "outside_data_object_range"
)

// DataObjResolution is the result of the data-object checks for one expression.
type DataObjResolution struct {
	Outcome DataObjResolutionOutcome
	// Coverage is the share of the read window that the held range covers, from 0 to 1. It is set
	// only when Outcome is DataObjOutcomeEligible.
	Coverage float64
}

// Eligible reports whether the expression passed every check.
func (r DataObjResolution) Eligible() bool {
	return r.Outcome == DataObjOutcomeEligible
}

// prefers reports whether the expression is eligible and the share of the read window that the held
// range covers is above DataObjCoverageThreshold.
func (r DataObjResolution) prefers() bool {
	return r.Eligible() && r.Coverage > DataObjCoverageThreshold
}

// dataObjResolve decides whether a querier may read the expression from data objects, with the
// settings of the frontend config. See ResolveDataObj.
func dataObjResolve(cfg Config, limits Limits, tenantIDs []string, expr syntax.Expr, start, end time.Time) DataObjResolution {
	return ResolveDataObj(cfg.DataObjEnabled, cfg.DataObjStorageLag, limits, tenantIDs, expr, start, end, cfg.now)
}

// ResolveDataObj decides whether a querier may read the expression from data objects. The request
// covers the time range from start to end.
//
// The checks mirror the ones the querier makes. Data objects must be enabled. The expression must be
// a stream-first sum. Every tenant must turn stream-first and data objects on. The read window must
// overlap the held range.
//
// The read window of a query is the time range the query reads. It is wider than the request,
// because it includes the range vector and moves back by the offset. The held range is the time
// range that data objects hold. It runs from the latest start time of the tenants to the current
// time minus the storage lag. A nil now means time.Now.
func ResolveDataObj(enabled bool, storageLag time.Duration, limits DataObjLimits, tenantIDs []string, expr syntax.Expr, start, end time.Time, now func() time.Time) DataObjResolution {
	if !enabled {
		return DataObjResolution{Outcome: DataObjOutcomeDataObjectsDisabled}
	}

	rng, ok := logql.StreamFirstRangeAggregationExpr(expr)
	if !ok {
		return DataObjResolution{Outcome: DataObjOutcomeUnsupportedQuery}
	}

	return dataObjResolveWindow(enabled, storageLag, limits, tenantIDs, rng.Left.Interval, rng.Left.Offset, start, end, now)
}

// dataObjAnyPrefers predicts whether a piece of a query will prefer data objects. The caller has
// not rewritten the query yet.
//
// It reports true only when data objects are on and every tenant turns stream-first and data
// objects on. It also needs both of these:
//   - A sub-expression has the stream-first shape, such as one leg of a binary expression.
//   - Data objects hold more than DataObjCoverageThreshold of the read window. The window uses the
//     longest range vector and the longest offset of the whole query.
//
// The query-frontend splits a query in time before it shards it, so it cannot yet see the pieces
// that a querier will run. A wrong prediction changes only how it splits the query.
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
	return dataObjResolveWindow(cfg.DataObjEnabled, cfg.DataObjStorageLag, limits, tenantIDs, maxRange, maxOffset, start, end, cfg.now).prefers()
}

// dataObjResolveWindow runs the checks that depend on the settings, the tenants and the time range.
// The query reads a range vector of the given length at the given offset.
func dataObjResolveWindow(enabled bool, storageLag time.Duration, limits DataObjLimits, tenantIDs []string, interval, offset time.Duration, start, end time.Time, now func() time.Time) DataObjResolution {
	if !enabled || len(tenantIDs) == 0 {
		return DataObjResolution{Outcome: DataObjOutcomeDataObjectsDisabled}
	}

	for _, id := range tenantIDs {
		if !limits.StreamFirstExecutionEnabled(id) {
			return DataObjResolution{Outcome: DataObjOutcomeStreamFirstDisabled}
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
			return DataObjResolution{Outcome: DataObjOutcomeDataObjectsDisabled}
		}
		if tenantStart.After(heldStart) {
			heldStart = tenantStart
		}
	}

	if now == nil {
		now = time.Now
	}
	heldEnd := now().Add(-storageLag)

	// The querier reads data objects only when the read window overlaps the held range. An empty
	// overlap makes the query ineligible. A non-empty overlap also proves that the window has a
	// positive length, so the division below is safe.
	overlapStart, overlapEnd := maxTime(windowStart, heldStart), minTime(windowEnd, heldEnd)
	if !overlapStart.Before(overlapEnd) {
		return DataObjResolution{Outcome: DataObjOutcomeOutsideDataObjRange}
	}

	return DataObjResolution{
		Outcome:  DataObjOutcomeEligible,
		Coverage: float64(overlapEnd.Sub(overlapStart)) / float64(windowEnd.Sub(windowStart)),
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

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
