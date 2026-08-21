// Package compare turns two benchmark reports into a side-by-side markdown
// comparison. Values are normalized per query so reports whose run counts differ
// stay comparable.
//
// This file holds the report-matching logic: pairing each query in one report
// with the same query in the other. The markdown generation lives in render.go.
package compare

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/report"
)

// alignQueries returns the ordered union of query keys across the two reports
// and the lookup maps from key to query. The order lists a's queries first (in
// a's order), then queries found only in b.
func alignQueries(a, b *report.Report) (order []string, aByKey, bByKey map[string]*report.Query) {
	aByKey = indexByKey(a)
	bByKey = indexByKey(b)
	seen := map[string]bool{}
	for i := range a.Queries {
		k := key(&a.Queries[i])
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
	}
	for i := range b.Queries {
		k := key(&b.Queries[i])
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
	}
	return order, aByKey, bByKey
}

// indexByKey maps each query to its identity key. On a duplicate key the last
// query wins, which is stable because the set is fixed.
func indexByKey(r *report.Report) map[string]*report.Query {
	m := make(map[string]*report.Query, len(r.Queries))
	for i := range r.Queries {
		m[key(&r.Queries[i])] = &r.Queries[i]
	}
	return m
}

// dataWindow is the span of data a query read (End - Start), rounded to whole
// seconds. Both the identity key and the rendered window column derive from it,
// so the value used to match a query and the value shown for it cannot drift.
func dataWindow(q *report.Query) time.Duration {
	return q.End.Sub(q.Start).Round(time.Second)
}

// key is a query's identity for matching across reports: the fields that define
// the workload, independent of the absolute wall-clock time it ran at.
func key(q *report.Query) string {
	windowSec := int64(dataWindow(q) / time.Second)
	stepSec := int64(math.Round(q.StepSeconds))
	return strings.Join([]string{
		string(q.Type),
		q.Expr,
		strconv.FormatInt(windowSec, 10),
		strconv.FormatInt(stepSec, 10),
	}, "|")
}
