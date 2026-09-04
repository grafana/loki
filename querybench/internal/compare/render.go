package compare

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/queries"
	"github.com/grafana/loki-query-benchmark/internal/report"
)

// Input names one report for the comparison: a short label and the parsed
// report it labels.
type Input struct {
	Name   string
	Report *report.Report
}

// RenderMarkdown returns the markdown comparison of a and b. Column cells read
// "<a-value> / <b-value> (±x%)", where the percentage is b relative to a and is
// omitted when either side is missing or a is zero. A query present in only one
// report still gets a row, with a dash for the absent side.
func RenderMarkdown(a, b Input) string {
	var sb strings.Builder
	writeHeader(&sb, a, b)
	writeTable(&sb, a, b)
	writeNotes(&sb)
	return sb.String()
}

// column is one metric in the comparison table: how to pull its per-query value
// from a query and how to render that value.
type column struct {
	header  string
	extract func(q *report.Query) (float64, bool)
	format  func(float64) string
}

var columns = []column{
	{header: "Min latency", extract: latencyPercentile(0), format: formatDuration},
	{header: "50p latency", extract: latencyPercentile(50), format: formatDuration},
	{header: "Max latency", extract: latencyPercentile(100), format: formatDuration},
	{header: "Processed bytes", extract: perQueryTotal(func(q *report.Query) (float64, bool) {
		return float64(q.QueryStats.ProcessedBytes), true
	}), format: formatBytes},
	{header: "Fetched bytes (object storage)", extract: perQueryTotal(fromUint(func(m report.SystemStats) *uint64 {
		return m.ObjstoreFetchedBytes
	})), format: formatBytes},
	{header: "Fetched bytes (memcached)", extract: perQueryTotal(fromUint(func(m report.SystemStats) *uint64 {
		return m.MemcachedWrittenBytes
	})), format: formatBytes},
	{header: "Object storage requests", extract: perQueryTotal(fromUint(func(m report.SystemStats) *uint64 {
		return m.ObjstoreRequests
	})), format: formatCount},
	// CPU seconds is a window total, so it is per-query normalized like the other
	// totals. Peak cores, heap peak and allocation rate are peaks or rates,
	// already run-count independent, and compared as-is.
	{header: "Querier CPU (s/query)", extract: perQueryTotal(fromFloat(func(m report.SystemStats) *float64 { return m.CPUSeconds })), format: formatDuration},
	{header: "Querier peak CPU (cores)", extract: fromFloat(func(m report.SystemStats) *float64 { return m.CPUPeakCores }), format: formatCores},
	{header: "Querier mem peak", extract: fromUint(func(m report.SystemStats) *uint64 { return m.HeapInusePeakBytes }), format: formatBytes},
	{header: "Querier mem alloc", extract: fromUint(func(m report.SystemStats) *uint64 { return m.AllocBytesPerSecond }), format: formatBytesPerSecond},
}

// fixedHeaders are the descriptive columns that precede the metric columns.
// Both the markdown and CSV renderers build their header from this plus
// columnHeaders(), so the two formats share one column order.
var fixedHeaders = []string{"Query type", "Query name", "Query expression", "Query timerange", "Query steps"}

// RenderCSV returns the comparison table as CSV, with the same columns as the
// markdown table. The expression and name cells are raw (no markdown code
// quoting or escaping); the CSV writer quotes any field that needs it. It omits
// the markdown header and notes: the table only.
func RenderCSV(a, b Input) (string, error) {
	order, aByKey, bByKey := alignQueries(a.Report, b.Report)

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(slices.Concat(fixedHeaders, columnHeaders())); err != nil {
		return "", err
	}
	for _, k := range order {
		qa, qb := aByKey[k], bByKey[k]
		ref := qa
		if ref == nil {
			ref = qb
		}

		row := []string{
			string(ref.Type),
			ref.Name,
			ref.Expr,
			timerangeCell(ref),
			stepCell(ref),
		}
		for _, c := range columns {
			row = append(row, cell(c, qa, qb))
		}
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

// writeTable writes the comparison table: one row per unique query, ordered by
// a's queries first, then queries found only in b.
func writeTable(sb *strings.Builder, a, b Input) {
	order, aByKey, bByKey := alignQueries(a.Report, b.Report)

	headers := slices.Concat(fixedHeaders, columnHeaders())
	sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	sb.WriteString("|" + strings.Repeat(" --- |", len(headers)) + "\n")

	for _, k := range order {
		qa, qb := aByKey[k], bByKey[k]
		ref := qa
		if ref == nil {
			ref = qb
		}

		cells := []string{
			string(ref.Type),
			mdEscape(ref.Name),
			exprCell(ref),
			timerangeCell(ref),
			stepCell(ref),
		}
		for _, c := range columns {
			cells = append(cells, cell(c, qa, qb))
		}
		sb.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
}

// cell renders one metric cell for the pair (qa, qb).
func cell(c column, qa, qb *report.Query) string {
	av, aok := extractOrMissing(c, qa)
	bv, bok := extractOrMissing(c, qb)

	as, bs := "–", "–"
	if aok {
		as = c.format(av)
	}
	if bok {
		bs = c.format(bv)
	}
	out := as + " / " + bs
	if aok && bok && av != 0 {
		out += fmt.Sprintf(" (%+.1f%%)", (bv-av)/av*100)
	}
	return out
}

// extractOrMissing returns a column's value for q, treating a nil query or a
// value the extractor cannot produce as missing.
func extractOrMissing(c column, q *report.Query) (float64, bool) {
	if q == nil {
		return 0, false
	}
	return c.extract(q)
}

func columnHeaders() []string {
	h := make([]string, len(columns))
	for i, c := range columns {
		h[i] = c.header
	}
	return h
}

// latencyPercentile returns an extractor for the p-th percentile of a query's
// per-run latencies. It is missing when the query recorded no successful run.
func latencyPercentile(p int) func(q *report.Query) (float64, bool) {
	return func(q *report.Query) (float64, bool) {
		if len(q.LatenciesSeconds) == 0 {
			return 0, false
		}
		return percentile(q.LatenciesSeconds, p), true
	}
}

// perQueryTotal wraps an additive-total extractor and divides its value by the
// run count, producing a per-query figure comparable across differing run
// counts.
func perQueryTotal(inner func(q *report.Query) (float64, bool)) func(q *report.Query) (float64, bool) {
	return func(q *report.Query) (float64, bool) {
		v, ok := inner(q)
		if !ok {
			return 0, false
		}
		if q.Runs <= 0 {
			return 0, false
		}
		return v / float64(q.Runs), true
	}
}

// fromFloat adapts a *float64 system-stat field to an extractor, missing when
// the metric was not captured.
func fromFloat(pick func(m report.SystemStats) *float64) func(q *report.Query) (float64, bool) {
	return func(q *report.Query) (float64, bool) {
		if v := pick(q.SystemMetrics); v != nil {
			return *v, true
		}
		return 0, false
	}
}

// fromUint adapts a *uint64 system-stat field to an extractor, missing when the
// metric was not captured.
func fromUint(pick func(m report.SystemStats) *uint64) func(q *report.Query) (float64, bool) {
	return func(q *report.Query) (float64, bool) {
		if v := pick(q.SystemMetrics); v != nil {
			return float64(*v), true
		}
		return 0, false
	}
}

// exprCell renders the expression column as inline code.
func exprCell(q *report.Query) string {
	return "`" + mdEscape(q.Expr) + "`"
}

// timerangeCell renders the query's data time range: the span of data actually
// read (the window plus the longest range vector). It distinguishes two queries
// that share an expression but span different ranges.
func timerangeCell(q *report.Query) string {
	return shortDuration(dataWindow(q))
}

// stepCell renders the step column, blank for instant queries.
func stepCell(q *report.Query) string {
	if q.Type != queries.TypeRange || q.StepSeconds == 0 {
		return "–"
	}
	return shortDuration(time.Duration(q.StepSeconds) * time.Second)
}

func writeHeader(sb *strings.Builder, a, b Input) {
	sb.WriteString("# Query benchmark comparison\n\n")
	sb.WriteString(fmt.Sprintf("Comparing **%s** (a) vs **%s** (b).\n\n", a.Name, b.Name))
	writeRunLine(sb, "a", a)
	writeRunLine(sb, "b", b)
	sb.WriteString("\n")
}

// writeRunLine writes one report's parameters as a bullet.
func writeRunLine(sb *strings.Builder, side string, in Input) {
	r := in.Report
	sb.WriteString(fmt.Sprintf("- **%s** — `%s`: %d runs/query, %s .. %s",
		in.Name, side, runsPerQuery(r),
		r.RequestedStart.UTC().Format(time.RFC3339),
		r.RequestedEnd.UTC().Format(time.RFC3339)))
	if r.Description != "" {
		sb.WriteString(" — " + strings.TrimSpace(r.Description))
	}
	sb.WriteString("\n")
}

// runsPerQuery reports the run count the report used. The set shares one count,
// so the first query's is representative; it is zero for an empty report.
func runsPerQuery(r *report.Report) int {
	if len(r.Queries) == 0 {
		return 0
	}
	return r.Queries[0].Runs
}

func writeNotes(sb *strings.Builder) {
	sb.WriteString("\nNotes:\n\n")
	sb.WriteString("- Each cell is `a / b (±% of b vs a)`.\n")
	sb.WriteString("- All figures are per single query.\n")
	sb.WriteString("- Latency min/50p/max come from the per-run latencies.\n")
	sb.WriteString("- Processed bytes come from the query responses; fetched bytes, object-storage requests and CPU seconds come from the metrics window; all are summed and divided by the run count.\n")
	sb.WriteString("- Querier peak CPU, memory peak and allocation rate are peaks or rates, already independent of the run count, so they are shown as captured.\n")
	sb.WriteString("- A `–` marks a query absent from one report or a metric that could not be captured; the percentage is omitted when either side is missing or the `a` value is zero.\n")
}

// percentile returns the p-th percentile of vals by index scaling: it indexes
// (p*(len-1))/100 into a sorted copy, so p=0 is the minimum, 50 the median and
// 100 the maximum. vals is not modified.
func percentile(vals []float64, p int) float64 {
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	idx := (p * (len(s) - 1)) / 100
	return s[idx]
}

// mdEscape escapes the characters that would break a markdown table cell.
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, `|`, `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
