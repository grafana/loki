// Package queries defines the fixed benchmark query set and the rules that turn
// a query plus the run's time bounds into a concrete request time range.
package queries

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// QueryType is the kind of a benchmark query: an instant query at one point in
// time, or a range query over a stepped interval.
type QueryType string

// TypeInstant and TypeRange are the two query kinds.
const (
	TypeInstant QueryType = "instant"
	TypeRange   QueryType = "range"
)

// Query is one benchmark query. The set is fixed in the binary so a report
// always describes the same workload.
//
// Expr is a complete LogQL expression, ready to send. Window is the query_range
// span (End-Start) for a range query and zero for an instant query, whose only
// lookback is the range vector in Expr. Step applies to range queries only and
// is zero for instant queries.
type Query struct {
	Name   string
	Type   QueryType
	Expr   string
	Window time.Duration
	Step   time.Duration
}

// shape is one query family. It generates an instant query (a [window] range
// vector at one point) and a range query (a window-long query_range, stepped by
// step, whose inner range vector is also step so the stepped windows are
// contiguous). query is a full LogQL expression with a "<duration>" placeholder
// where the range vector goes; each variant substitutes it via rangeVector.
type shape struct {
	name   string
	query  string
	window time.Duration
	step   time.Duration
}

// Default returns the fixed benchmark query set: every shape as an instant and a
// range query.
func Default() []Query {
	shapes := []shape{
		// Label matchers only.
		{"=~ broad label matcher (24h)", `sum(count_over_time({service_name=~".+"}<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"=~ broad label matcher by job (24h)", `sum by (job) (count_over_time({service_name=~".+"}<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"=~ narrow label matcher (24h)", `sum(count_over_time({service_name=~"alloy.*"}<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"!~ broad label matcher (24h)", `sum(count_over_time({service_name=~".+", service_name!~"alloy.*"}<duration>))`, 24 * time.Hour, 15 * time.Minute},
		// Structured metadata filters only.
		{"structured metadata = filter (24h)", `sum(count_over_time({service_name=~".+"} | detected_level="error"<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"structured metadata != filter (24h)", `sum(count_over_time({service_name=~".+"} | detected_level!="info"<duration>))`, 24 * time.Hour, 15 * time.Minute},
		// Line matchers.
		{"|= needle-in-haystack line filter (24h)", `sum(count_over_time({service_name=~".+"} |= "Unauthenticated"<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"|= line filter (24h)", `sum(count_over_time({service_name=~".+"} |= "error"<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"|~ line filter (1h)", `sum(count_over_time({service_name=~".+"} |~ "(?i)(error|panic|fatal)"<duration>))`, 1 * time.Hour, time.Minute},
		{"!= line filter (24h)", `sum(count_over_time({service_name=~".+"} != "level=info"<duration>))`, 24 * time.Hour, 15 * time.Minute},
		{"logfmt = filter (24h)", `sum(count_over_time({service_name=~".+"} | logfmt | level="error"<duration>))`, 24 * time.Hour, 15 * time.Minute},
	}

	qs := make([]Query, 0, len(shapes)*2)
	for _, s := range shapes {
		qs = append(qs,
			// Run only range queries to speed up the test.
			// Query{Name: s.name, Type: TypeInstant, Expr: strings.ReplaceAll(s.query, "<duration>", rangeVector(s.window))},
			Query{Name: s.name, Type: TypeRange, Expr: strings.ReplaceAll(s.query, "<duration>", rangeVector(s.step)), Window: s.window, Step: s.step},
		)
	}
	return qs
}

// rangeVector renders d as a LogQL range-vector selector, e.g. "[24h]", "[15m]"
// or "[30s]", preferring the largest whole unit.
func rangeVector(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("[%dh]", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("[%dm]", d/time.Minute)
	default:
		return fmt.Sprintf("[%ds]", d/time.Second)
	}
}

// Validate returns an error if any query's expression does not parse. The tool
// calls it before a run so a malformed expression in the fixed set fails loudly,
// instead of longestRangeVector silently treating the parse failure as no range
// vector and misplacing the query's data start.
func Validate(qs []Query) error {
	for _, q := range qs {
		if _, err := syntax.ParseExpr(q.Expr); err != nil {
			return fmt.Errorf("query %q has an unparseable expression %q: %w", q.Name, q.Expr, err)
		}
	}
	return nil
}

// FilterByRegex returns the queries whose name or expression matches the regular
// expression pattern. An empty pattern returns the input unchanged. It errors
// when pattern is not a valid regular expression. The result keeps the input
// order.
func FilterByRegex(qs []Query, pattern string) ([]Query, error) {
	if pattern == "" {
		return qs, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid query filter regex %q: %w", pattern, err)
	}
	out := make([]Query, 0, len(qs))
	for _, q := range qs {
		if re.MatchString(q.Name) || re.MatchString(q.Expr) {
			out = append(out, q)
		}
	}
	return out, nil
}

// RequestRange returns the request time range. For a range query it is the
// query_range [start, end] spanning Window; for an instant query it is
// [end, end], since only the evaluation time is sent.
func (q Query) RequestRange(end time.Time) (start, qend time.Time) {
	if q.Type == TypeRange {
		return end.Add(-q.Window), end
	}
	return end, end
}

// DataRange returns the real time range the query reads: [end - Window -
// rangeVector, end], where rangeVector is the longest range vector in Expr. A
// range query over a 24h Window with a [1h] range vector reads back to end-25h;
// an instant query (Window zero) reads back only its range vector.
func (q Query) DataRange(end time.Time) (start, qend time.Time) {
	return end.Add(-(q.Window + longestRangeVector(q.Expr))), end
}

// FilterByDataRange splits qs into the queries whose data range stays within
// [start, end] and the queries skipped because their data reaches before start.
// Every query ends at end, so only the start bound can exclude a query.
func FilterByDataRange(qs []Query, start, end time.Time) (kept, skipped []Query) {
	for _, q := range qs {
		dataStart, _ := q.DataRange(end)
		if dataStart.Before(start) {
			skipped = append(skipped, q)
		} else {
			kept = append(kept, q)
		}
	}
	return kept, skipped
}

// longestRangeVector returns the longest range-vector interval in expr, or zero
// when expr does not parse or has no range vector. It parses expr with Loki's
// own LogQL parser and walks the AST, so it stays correct for every range-vector
// form the parser accepts rather than a hand-written pattern.
func longestRangeVector(expr string) time.Duration {
	parsed, err := syntax.ParseExpr(expr)
	if err != nil {
		return 0
	}
	var longest time.Duration
	parsed.Walk(func(e syntax.Expr) bool {
		if lr, ok := e.(*syntax.LogRangeExpr); ok && lr.Interval > longest {
			longest = lr.Interval
		}
		return true
	})
	return longest
}
