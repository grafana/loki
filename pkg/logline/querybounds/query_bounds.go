package querybounds

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type QueryType string

const (
	QueryTypeMetricRange    QueryType = "metric_range"
	QueryTypeMetricInstant  QueryType = "metric_instant"
	QueryTypeLog            QueryType = "log"
	QueryTypeDetectedFields QueryType = "detected_fields"
)

func GetQueryType(req queryrangebase.Request, expr syntax.Expr) QueryType {
	_, isDF := req.(*queryrange.DetectedFieldsRequest)
	_, isRange := req.(*queryrange.LokiRequest)
	_, isInstant := req.(*queryrange.LokiInstantRequest)
	_, isMetric := expr.(syntax.SampleExpr)
	switch {
	case isDF:
		return QueryTypeDetectedFields
	case isRange && isMetric:
		return QueryTypeMetricRange
	case isInstant && isMetric:
		return QueryTypeMetricInstant
	default:
		return QueryTypeLog
	}
}

type QueryBounds struct {
	RequestedStart, RequestedEnd time.Time // what you send to Loki
	ReadStart, ReadEnd           time.Time // aligned, then widened
}

func GetQueryBounds(
	reqStart, reqEnd time.Time,
	step int64,
	expr syntax.Expr,
	qt QueryType,
	alignQueriesWithStep bool,
) QueryBounds {
	alStart, alEnd := alignStartEnd(reqStart, reqEnd, step, qt, alignQueriesWithStep)
	readStart, readEnd := readBounds(expr, alStart, alEnd)
	return QueryBounds{
		RequestedStart: reqStart.UTC(),
		RequestedEnd:   reqEnd.UTC(),
		ReadStart:      readStart.UTC(),
		ReadEnd:        readEnd.UTC(),
	}
}

func alignStartEnd(
	reqStart, reqEnd time.Time,
	step int64,
	qt QueryType,
	alignQueriesWithStep bool,
) (time.Time, time.Time) {
	if qt == QueryTypeMetricRange {
		return alignMetricBounds(reqStart, reqEnd, step, alignQueriesWithStep)
	}
	return reqStart, reqEnd
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
func alignMetricBounds(reqStart, reqEnd time.Time, step int64, alignQueriesWithStep bool) (time.Time, time.Time) {
	start, end := reqStart, reqEnd
	if step <= 0 {
		return start, end
	}
	if alignQueriesWithStep {
		start, end = alignStep(start, end, step)
	}
	return roundStep(start, end, step)
}

func alignStep(start, end time.Time, step int64) (time.Time, time.Time) {
	floorStart := (start.UnixMilli() / step) * step
	floorEnd := (end.UnixMilli() / step) * step
	return time.UnixMilli(floorStart), time.UnixMilli(floorEnd)
}

func roundStep(start, end time.Time, step int64) (time.Time, time.Time) {
	stepNs := step * 1e6
	startNs := start.UnixNano()

	endNs := end.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}

	return time.Unix(0, startNs-startNs%stepNs), time.Unix(0, endNs)
}

// readBounds moves the bounds back by each group's range and offset and returns their union.
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
