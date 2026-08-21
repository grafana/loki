package queries

import (
	"strings"
	"testing"
	"time"
)

func TestDefault_ShapeIsConsistent(t *testing.T) {
	for _, q := range Default() {
		if q.Type != TypeInstant && q.Type != TypeRange {
			t.Errorf("%s: bad type %q", q.Name, q.Type)
		}
		if q.Type == TypeInstant && (q.Window != 0 || q.Step != 0) {
			t.Errorf("%s: instant query must have zero window and step, got window=%s step=%s", q.Name, q.Window, q.Step)
		}
		if q.Type == TypeRange && (q.Window <= 0 || q.Step <= 0) {
			t.Errorf("%s: range query needs a positive window and step, got window=%s step=%s", q.Name, q.Window, q.Step)
		}
		if longestRangeVector(q.Expr) <= 0 {
			t.Errorf("%s: expr %q parsed to no range vector", q.Name, q.Expr)
		}
		if !strings.Contains(q.Expr, "service_name") {
			t.Errorf("%s: expr %q does not select on service_name", q.Name, q.Expr)
		}
	}
}

func TestDefault_AllExpressionsParse(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatalf("default query set must all parse: %v", err)
	}
}

func TestFilterByRegex(t *testing.T) {
	qs := Default()

	all, err := FilterByRegex(qs, "")
	if err != nil || len(all) != len(qs) {
		t.Fatalf("empty pattern: got %d (err %v), want %d and no error", len(all), err, len(qs))
	}

	// "needle" appears only in a query name, not in any expression.
	byName, err := FilterByRegex(qs, "needle")
	if err != nil {
		t.Fatalf("byName: %v", err)
	}
	if len(byName) == 0 {
		t.Fatal("regex against the name matched nothing")
	}
	for _, q := range byName {
		if !strings.Contains(q.Name, "needle") {
			t.Errorf("name filter leaked %q", q.Name)
		}
	}

	// "detected_level" appears only in expressions, not in any name.
	byExpr, err := FilterByRegex(qs, "detected_level")
	if err != nil {
		t.Fatalf("byExpr: %v", err)
	}
	if len(byExpr) == 0 {
		t.Fatal("regex against the expression matched nothing")
	}
	for _, q := range byExpr {
		if !strings.Contains(q.Expr, "detected_level") {
			t.Errorf("expression filter leaked %q", q.Name)
		}
	}

	// Alternation matches the union of the name and expression hits (disjoint here).
	both, err := FilterByRegex(qs, "needle|detected_level")
	if err != nil {
		t.Fatalf("alternation: %v", err)
	}
	if len(both) != len(byName)+len(byExpr) {
		t.Errorf("alternation matched %d, want %d", len(both), len(byName)+len(byExpr))
	}

	if _, err := FilterByRegex(qs, "["); err == nil {
		t.Error("invalid regex must return an error")
	}

	if got, _ := FilterByRegex(qs, "nope-no-match"); len(got) != 0 {
		t.Errorf("non-matching regex returned %d queries", len(got))
	}
}

func TestDataRange_IncludesRangeVector(t *testing.T) {
	end := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	// A range query reaches back Window plus its range vector: the user's example
	// of a 24h window with a [1h] range vector reads from end-25h.
	rng := Query{Type: TypeRange, Expr: `sum(count_over_time({app="x"}[1h]))`, Window: 24 * time.Hour, Step: 15 * time.Minute}
	start, qend := rng.DataRange(end)
	if want := end.Add(-25 * time.Hour); !start.Equal(want) {
		t.Errorf("range data start = %s, want %s", start, want)
	}
	if !qend.Equal(end) {
		t.Errorf("range data end = %s, want %s", qend, end)
	}

	// An instant query has no window; it reaches back only its range vector.
	inst := Query{Type: TypeInstant, Expr: `sum(count_over_time({app="x"}[24h]))`}
	start, _ = inst.DataRange(end)
	if want := end.Add(-24 * time.Hour); !start.Equal(want) {
		t.Errorf("instant data start = %s, want %s", start, want)
	}
}

func TestRequestRange(t *testing.T) {
	end := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	// A range query requests its query_range span, ignoring the range vector.
	rng := Query{Type: TypeRange, Expr: `sum(count_over_time({app="x"}[1h]))`, Window: 24 * time.Hour}
	start, qend := rng.RequestRange(end)
	if want := end.Add(-24 * time.Hour); !start.Equal(want) || !qend.Equal(end) {
		t.Errorf("range request range = [%s, %s], want [%s, %s]", start, qend, want, end)
	}

	// An instant query requests only its evaluation time.
	inst := Query{Type: TypeInstant, Expr: `sum(count_over_time({app="x"}[1h]))`}
	start, qend = inst.RequestRange(end)
	if !start.Equal(end) || !qend.Equal(end) {
		t.Errorf("instant request range = [%s, %s], want [%s, %s]", start, qend, end, end)
	}
}

func TestFilterByDataRange_SkipsQueriesReachingBeforeStart(t *testing.T) {
	end := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	start := end.Add(-24 * time.Hour)

	tooWide := Query{Name: "wide", Type: TypeRange, Expr: `sum(count_over_time({app="x"}[1h]))`, Window: 24 * time.Hour, Step: time.Minute} // end-25h
	fits := Query{Name: "fits", Type: TypeRange, Expr: `sum(count_over_time({app="x"}[5m]))`, Window: 6 * time.Hour, Step: time.Minute}     // end-6h5m
	boundary := Query{Name: "boundary", Type: TypeInstant, Expr: `sum(count_over_time({app="x"}[24h]))`}                                    // end-24h == start

	kept, skipped := FilterByDataRange([]Query{tooWide, fits, boundary}, start, end)

	if len(kept) != 2 || kept[0].Name != "fits" || kept[1].Name != "boundary" {
		t.Errorf("kept = %v, want [fits boundary]", names(kept))
	}
	if len(skipped) != 1 || skipped[0].Name != "wide" {
		t.Errorf("skipped = %v, want [wide]", names(skipped))
	}
}

func names(qs []Query) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.Name
	}
	return out
}

func TestLongestRangeVector(t *testing.T) {
	cases := map[string]time.Duration{
		`sum(count_over_time({app="x"}[5m]))`:        5 * time.Minute,
		`rate({app="x"}[1h]) + rate({app="x"}[30m])`: time.Hour, // the longest of several
		`sum(count_over_time({app="x"}[1h30m]))`:     time.Hour + 30*time.Minute,
		`{app="x"}`:                                  0, // log selector, no range vector
		`sum(count_over_time({app="x"`:               0, // does not parse
	}
	for expr, want := range cases {
		if got := longestRangeVector(expr); got != want {
			t.Errorf("longestRangeVector(%q) = %s, want %s", expr, got, want)
		}
	}
}
