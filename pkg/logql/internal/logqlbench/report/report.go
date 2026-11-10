// Command report turns `go test -bench BenchmarkLogQLMetricQueries` output into the markdown
// tables pasted into a PR description. It reads from stdin, or from the file named by its one
// argument, and writes markdown to stdout.
package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// scenarios is the fixed display order for every cell: values are listed in this order, one per
// line, and the order is stated once before each table rather than repeated in every cell.
var scenarios = []string{"chunk-timestamp-first", "chunk-stream-first", "dataobj-stream-first"}

// latencies is the fixed column order for the results table.
var latencies = []string{"0s", "50ms", "250ms"}

// maxParallelLatency is the latency at which the results table reports peak read parallelism: the
// highest one, where reads overlap the most.
var maxParallelLatency = latencies[len(latencies)-1]

// queryKey identifies one query shape, independent of scenario and latency.
type queryKey struct {
	expr, shape, kind string
}

// sums accumulates one metric's total and count, so duplicate (query, scenario, latency) lines —
// from a -count>1 run, or concatenated output — average together instead of the last one winning.
type sums struct {
	total float64
	n     int
}

func (s *sums) add(v float64) { s.total += v; s.n++ }
func (s *sums) mean() float64 {
	if s.n == 0 {
		return 0
	}
	return s.total / float64(s.n)
}

// leaf accumulates every metric seen for one (query, scenario, latency) combination.
type leaf struct {
	metrics map[string]*sums
}

func newLeaf() *leaf { return &leaf{metrics: make(map[string]*sums)} }

func (l *leaf) add(unit string, v float64) {
	s, ok := l.metrics[unit]
	if !ok {
		s = &sums{}
		l.metrics[unit] = s
	}
	s.add(v)
}

func (l *leaf) mean(unit string) (float64, bool) {
	s, ok := l.metrics[unit]
	if !ok {
		return 0, false
	}
	return s.mean(), true
}

// report holds every parsed leaf, plus the order queries first appeared in, so table rows follow
// the order the benchmark ran them in.
type report struct {
	order []queryKey
	seen  map[queryKey]bool
	leafs map[queryKey]map[string]map[string]*leaf // query -> scenario -> latency -> leaf
}

func newReport() *report {
	return &report{seen: map[queryKey]bool{}, leafs: map[queryKey]map[string]map[string]*leaf{}}
}

func (r *report) leafFor(q queryKey, scenario, latency string) *leaf {
	if !r.seen[q] {
		r.seen[q] = true
		r.order = append(r.order, q)
		r.leafs[q] = map[string]map[string]*leaf{}
	}
	byLatency, ok := r.leafs[q][scenario]
	if !ok {
		byLatency = map[string]*leaf{}
		r.leafs[q][scenario] = byLatency
	}
	l, ok := byLatency[latency]
	if !ok {
		l = newLeaf()
		byLatency[latency] = l
	}
	return l
}

// lineRE splits a benchmark result line into its query segment, scenario, latency, and the
// trailing "<value> <unit>" metrics. The query segment can itself contain "_" from spaces (LogQL
// syntax like "sum by(x) (...)" prints as "sum_by(x)_(...)"), so it is parsed separately by
// parseQuerySegment rather than by this regexp.
// The GOMAXPROCS suffix ("-11") is absent when GOMAXPROCS is 1, e.g. from a `go test -cpu 1` run.
var lineRE = regexp.MustCompile(`^BenchmarkLogQLMetricQueries/(query=.+)/scenario=([a-z-]+)/latency=([0-9a-z]+)(?:-\d+)?\s+\d+\s+(.*)$`)

// metricRE matches each "<value> <unit>" pair in a benchmark result line's metrics tail.
var metricRE = regexp.MustCompile(`([0-9.]+)\s+([\w/]+)`)

// parseQuerySegment splits "query=<expr>_(<shape>,_<kind>)" into expr, shape, and kind.
//
// The description is always the last parenthesized group, appended as " (" + description + ")"
// with no parens of its own, so it is found by taking the text between the last "(" and the
// final ")" — not by searching for the first "(", which belongs to the expression.
func parseQuerySegment(seg string) (expr, shape, kind string, ok bool) {
	rest := strings.TrimPrefix(seg, "query=")
	if !strings.HasSuffix(rest, ")") {
		return "", "", "", false
	}
	open := strings.LastIndex(rest, "(")
	if open < 0 {
		return "", "", "", false
	}
	inner := rest[open+1 : len(rest)-1]
	parts := strings.Split(inner, ",")
	if len(parts) < 2 {
		return "", "", "", false
	}
	kind = strings.TrimSpace(strings.ReplaceAll(parts[len(parts)-1], "_", " "))
	shape = strings.TrimSpace(strings.ReplaceAll(strings.Join(parts[:len(parts)-1], ","), "_", " "))
	expr = strings.ReplaceAll(strings.TrimSuffix(rest[:open], "_"), "_", " ")
	return expr, shape, kind, true
}

// parse reads every benchmark result line from r into a report, ignoring lines that do not match
// (headers, PASS/ok, blank lines).
func parse(r io.Reader) (*report, error) {
	rep := newReport()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		m := lineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		querySeg, scenario, latency, metricsPart := m[1], m[2], m[3], m[4]

		expr, shape, kind, ok := parseQuerySegment(querySeg)
		if !ok {
			continue
		}

		l := rep.leafFor(queryKey{expr: expr, shape: shape, kind: kind}, scenario, latency)
		for _, mm := range metricRE.FindAllStringSubmatch(metricsPart, -1) {
			v, err := strconv.ParseFloat(mm[1], 64)
			if err != nil {
				continue
			}
			l.add(mm[2], v)
		}
	}

	return rep, nil
}

// cell joins one value per scenario, in the fixed scenario order, one per line with <br>. A
// scenario this query never ran for reports as n/a.
func cell(rep *report, q queryKey, latency string, format func(*leaf) string) string {
	byScenario := rep.leafs[q]
	values := make([]string, len(scenarios))
	for i, scenario := range scenarios {
		l, ok := byScenario[scenario][latency]
		if !ok {
			values[i] = "n/a"
			continue
		}
		values[i] = format(l)
	}
	return strings.Join(values, "<br>")
}

func formatSeconds(l *leaf) string {
	v, ok := l.mean("ns/op")
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.2fs", v/1e9)
}

func formatCount(unit string) func(*leaf) string {
	return func(l *leaf) string {
		v, ok := l.mean(unit)
		if !ok {
			return "n/a"
		}
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
}

func formatMB(unit string) func(*leaf) string {
	return func(l *leaf) string {
		v, ok := l.mean(unit)
		if !ok {
			return "n/a"
		}
		return fmt.Sprintf("%.1fMB", v/1e6)
	}
}

func formatMillions(unit string) func(*leaf) string {
	return func(l *leaf) string {
		v, ok := l.mean(unit)
		if !ok {
			return "n/a"
		}
		return fmt.Sprintf("%.2fM", v/1e6)
	}
}

func writeScenarioOrderNote(w io.Writer) {
	fmt.Fprintln(w, "Each cell lists one value per scenario, in the order:")
	fmt.Fprintln(w)
	for _, s := range scenarios {
		fmt.Fprintf(w, "- %s\n", s)
	}
	fmt.Fprintln(w)
}

func generate(w io.Writer, rep *report) {
	fmt.Fprintln(w, "## Benchmark results")
	fmt.Fprintln(w)
	writeScenarioOrderNote(w)
	fmt.Fprint(w, "| Query (shape) | Type |")
	for _, latency := range latencies {
		fmt.Fprintf(w, " latency=%s |", latency)
	}
	fmt.Fprint(w, "\n|---|---|"+strings.Repeat("---|", len(latencies))+"\n")
	for _, q := range rep.order {
		fmt.Fprintf(w, "| `%s`<br>%s | %s |", q.expr, q.shape, q.kind)
		for _, latency := range latencies {
			fmt.Fprintf(w, " %s |", cell(rep, q, latency, formatSeconds))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w)
	fmt.Fprint(w, "| Query (shape) | Type | Store ops | Bytes fetched from storage | Max parallel reads | Memory allocations | Memory operations (bytes) |\n|---|---|---|---|---|---|---|\n")
	for _, q := range rep.order {
		fmt.Fprintf(w, "| `%s`<br>%s | %s | %s | %s | %s | %s | %s |\n",
			q.expr, q.shape, q.kind,
			cell(rep, q, "0s", formatCount("store_reqs/op")),
			cell(rep, q, "0s", formatMB("store_bytes/op")),
			cell(rep, q, maxParallelLatency, formatCount("store_max_parallel")),
			cell(rep, q, "0s", formatMillions("allocs/op")),
			cell(rep, q, "0s", formatMB("B/op")),
		)
	}
}

func main() {
	var in io.Reader = os.Stdin
	if len(os.Args) > 1 {
		f, err := os.Open(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		in = f
	}

	rep, err := parse(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(rep.order) == 0 {
		fmt.Fprintln(os.Stderr, "report: no BenchmarkLogQLMetricQueries result lines found in input")
		os.Exit(1)
	}

	generate(os.Stdout, rep)
}
