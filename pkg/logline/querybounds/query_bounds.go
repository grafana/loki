package querybounds

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type QueryBounds struct {
	RequestedStart, RequestedEnd time.Time // what you send to Loki
	ReadStart, ReadEnd           time.Time // aligned, then widened
}

func GetQueryBounds(req queryrangebase.Request, expr syntax.Expr, alignQueriesWithStep bool) QueryBounds {
	alStart, alEnd := alignStartEnd(req, expr, alignQueriesWithStep)
	readStart, readEnd := readBounds(expr, alStart, alEnd)
	return QueryBounds{
		RequestedStart: req.GetStart().UTC(),
		RequestedEnd:   req.GetEnd().UTC(),
		ReadStart:      readStart.UTC(),
		ReadEnd:        readEnd.UTC(),
	}
}

//------- Step 1: Align and round steps

func alignStartEnd(req queryrangebase.Request, expr syntax.Expr, alignQueriesWithStep bool) (time.Time, time.Time) {
	_, isRange := req.(*queryrange.LokiRequest)
	_, isMetric := expr.(syntax.SampleExpr)
	if isRange && isMetric {
		return alignMetricBounds(req, alignQueriesWithStep)
	}
	return req.GetStart(), req.GetEnd()
}

// alignMetricBounds matches Loki's range-metric order: optional StepAlign,
// then the splitter's alignStartEnd. Widen runs after this.
//
// Example: A 1m-step query from 02:00:10 to 02:05:10:
//
//	alignQueriesWithStep true: floor both ends, then ceil the end
//	  02:00:10–02:05:10 → 02:00:00–02:05:00 → 02:00:00–02:05:00
//	alignQueriesWithStep false: floor the start and ceil the end
//	  02:00:10–02:05:10 → 02:00:00–02:06:00
//
// A zero step is returned unchanged.
func alignMetricBounds(req queryrangebase.Request, alignQueriesWithStep bool) (time.Time, time.Time) {
	start, end, step := req.GetStart(), req.GetEnd(), req.GetStep()
	if step <= 0 {
		return start, end
	}
	if alignQueriesWithStep {
		start, end = alignStep(start, end, step)
	}
	return roundStep(start, end, step)
}

// If cfg.AlignQueriesWithStep = true, floor the
// start and end to the nearest step
func alignStep(start, end time.Time, step int64) (time.Time, time.Time) {
	floorStart := (start.UnixMilli() / step) * step
	floorEnd := (end.UnixMilli() / step) * step
	return time.UnixMilli(floorStart), time.UnixMilli(floorEnd)
}

// Round the start down to the nearest step and the end up.
// This mimics what the QF does with Loki queries in split_by_interval.
func roundStep(start, end time.Time, step int64) (time.Time, time.Time) {
	stepNs := step * 1e6
	startNs := start.UnixNano()

	endNs := end.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}

	return time.Unix(0, startNs-startNs%stepNs), time.Unix(0, endNs)
}

//------- Step 2: Widen read range to account for step intervals

func readBounds(expr syntax.Expr, start, end time.Time) (time.Time, time.Time) {
	groups, err := syntax.MatcherGroups(expr)
	if err != nil || len(groups) == 0 {
		return start, end
	}
	readStart, readEnd := groups[0].Widen(start, end)
	for _, group := range groups[1:] {
		widenedStart, widenedEnd := group.Widen(start, end)
		if widenedStart.Before(readStart) {
			readStart = widenedStart
		}
		if widenedEnd.After(readEnd) {
			readEnd = widenedEnd
		}
	}
	return readStart, readEnd
}
