package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// testAssertion handles comparing expected vs actual test results.
type testAssertion struct{}

// newTestAssertion creates a new test assertion helper.
func newTestAssertion() *testAssertion {
	return &testAssertion{}
}

// compareAlerts compares expected alerts with actual alerts.
func (ta *testAssertion) compareAlerts(testCase alertTestCase, actualAlerts []*activeAlert) error {
	expected := testCase.ExpAlerts

	// Sort both expected and actual for consistent comparison
	sortedExpected := sortExpectedAlerts(expected)
	sortedActual := sortActiveAlerts(actualAlerts)

	// Check counts
	if len(sortedExpected) != len(sortedActual) {
		return fmt.Errorf("\n  alertname: %s, time: %s,%s",
			testCase.Alertname,
			testCase.EvalTime,
			ta.formatAlertDiff(sortedExpected, sortedActual))
	}

	// Compare each alert
	for i := range sortedExpected {
		if err := ta.compareAlert(testCase.Alertname, sortedExpected[i], sortedActual[i]); err != nil {
			return fmt.Errorf("\n  alertname: %s, time: %s,\n    %v%s",
				testCase.Alertname,
				testCase.EvalTime,
				err,
				ta.formatAlertDiff(sortedExpected, sortedActual))
		}
	}

	return nil
}

// compareAlert compares a single expected alert with an actual alert.
func (ta *testAssertion) compareAlert(alertName string, expected alert, actual *activeAlert) error {
	// Compare labels
	expectedLabels := convertMapToLabels(expected.ExpLabels)
	if !labels.Equal(expectedLabels, actual.Labels) {
		return fmt.Errorf("alert %s label mismatch:\n  expected: %s\n  got: %s",
			alertName,
			expectedLabels.String(),
			actual.Labels.String())
	}

	// Compare annotations
	expectedAnnotations := convertMapToLabels(expected.ExpAnnotations)
	if !labels.Equal(expectedAnnotations, actual.Annotations) {
		return fmt.Errorf("alert %s annotation mismatch:\n  expected: %s\n  got: %s",
			alertName,
			expectedAnnotations.String(),
			actual.Annotations.String())
	}

	return nil
}

// compareLogQLSamples compares expected samples with actual query results.
func (ta *testAssertion) compareLogQLSamples(testCase logqlTestCase, result *logqlmodel.Result) error {
	if len(testCase.ExpSamples) == 0 {
		return nil // No samples to compare
	}

	// Handle different result types
	switch v := result.Data.(type) {
	case promql.Vector:
		return ta.compareVector(testCase, v)
	case promql.Scalar:
		return ta.compareScalar(testCase, v)
	case promql.Matrix:
		return fmt.Errorf("matrix results not yet supported in assertions")
	default:
		return fmt.Errorf("unexpected result type: %T", result.Data)
	}
}

// compareVector compares expected samples with a Vector result.
func (ta *testAssertion) compareVector(testCase logqlTestCase, vector promql.Vector) error {
	expected := testCase.ExpSamples

	// Sort for consistent comparison
	sortedVector := make(promql.Vector, len(vector))
	copy(sortedVector, vector)
	sort.Slice(sortedVector, func(i, j int) bool {
		return labels.Compare(sortedVector[i].Metric, sortedVector[j].Metric) < 0
	})

	// Check counts
	if len(expected) != len(sortedVector) {
		return fmt.Errorf("\n  expr: %s, time: %s,%s",
			testCase.Expr,
			testCase.EvalTime,
			ta.formatVectorDiff(expected, sortedVector))
	}

	// Compare each sample
	for i, exp := range expected {
		actual := sortedVector[i]

		// Parse expected labels
		expLabels, err := parseStreamLabels(exp.Labels)
		if err != nil {
			return fmt.Errorf("failed to parse expected labels: %w", err)
		}

		// Compare labels
		if !labels.Equal(expLabels, actual.Metric) {
			return fmt.Errorf("\n  expr: %s, time: %s,\n    sample %d label mismatch%s",
				testCase.Expr,
				testCase.EvalTime,
				i,
				ta.formatVectorDiff(expected, sortedVector))
		}

		// Compare value
		if err := ta.compareFloat(exp.Value, actual.F); err != nil {
			return fmt.Errorf("\n  expr: %s, time: %s,\n    sample %d value mismatch%s",
				testCase.Expr,
				testCase.EvalTime,
				i,
				ta.formatVectorDiff(expected, sortedVector))
		}
	}

	return nil
}

// compareScalar compares expected samples with a Scalar result.
func (ta *testAssertion) compareScalar(testCase logqlTestCase, scalar promql.Scalar) error {
	expected := testCase.ExpSamples

	if len(expected) != 1 {
		return fmt.Errorf("scalar result requires exactly 1 expected sample, got %d", len(expected))
	}

	exp := expected[0]

	// Compare value
	if err := ta.compareFloat(exp.Value, scalar.V); err != nil {
		return fmt.Errorf("scalar value mismatch: %v", err)
	}

	return nil
}

// compareLogQLLogs compares expected log lines with actual query results.
func (ta *testAssertion) compareLogQLLogs(testCase logqlTestCase, result *logqlmodel.Result) error {
	if len(testCase.ExpLogs) == 0 {
		return nil // No logs to compare
	}

	// Check if result is a streams type
	streams, ok := result.Data.(logqlmodel.Streams)
	if !ok {
		return fmt.Errorf("expected streams result for log query, got %T", result.Data)
	}

	// Flatten streams into individual log lines for comparison
	var actualLogs []logLineResult
	for _, stream := range streams {
		streamLabels, err := parser.ParseMetric(stream.Labels)
		if err != nil {
			continue
		}

		for _, entry := range stream.Entries {
			actualLogs = append(actualLogs, logLineResult{
				Labels:    streamLabels,
				Line:      entry.Line,
				Timestamp: entry.Timestamp.UnixNano(),
			})
		}
	}

	// Sort for consistent comparison
	sortLogLines(actualLogs)

	// Check counts
	if len(testCase.ExpLogs) != len(actualLogs) {
		return fmt.Errorf("log count mismatch:\n  expected: %d logs\n  got: %d logs",
			len(testCase.ExpLogs),
			len(actualLogs))
	}

	// Compare each log line
	for i, exp := range testCase.ExpLogs {
		actual := actualLogs[i]

		// Parse expected labels
		expLabels, err := parseStreamLabels(exp.Labels)
		if err != nil {
			return fmt.Errorf("failed to parse expected labels: %w", err)
		}

		// Compare labels
		if !labels.Equal(expLabels, actual.Labels) {
			return fmt.Errorf("log %d label mismatch:\n  expected: %s\n  got: %s",
				i,
				expLabels.String(),
				actual.Labels.String())
		}

		// Compare line content
		if exp.Line != actual.Line {
			return fmt.Errorf("log %d content mismatch:\n  expected: %q\n  got: %q",
				i,
				exp.Line,
				actual.Line)
		}

		// Compare timestamp if specified
		if exp.Timestamp != 0 && exp.Timestamp != actual.Timestamp {
			return fmt.Errorf("log %d timestamp mismatch:\n  expected: %d\n  got: %d",
				i,
				exp.Timestamp,
				actual.Timestamp)
		}
	}

	return nil
}

// compareFloat compares two float64 values.
func (ta *testAssertion) compareFloat(expected, actual float64) error {
	if expected != actual {
		return fmt.Errorf("expected %v, got %v", expected, actual)
	}
	return nil
}

// Helper types and functions

type logLineResult struct {
	Labels    labels.Labels
	Line      string
	Timestamp int64
}

// formatAlertDiff formats a diff between expected and actual alerts in promtool style.
func (ta *testAssertion) formatAlertDiff(expected []alert, actual []*activeAlert) string {
	var b strings.Builder

	// Format expected alerts
	b.WriteString("\n    exp:[\n")
	if len(expected) == 0 {
		b.WriteString("        ]\n")
	} else {
		for i, exp := range expected {
			b.WriteString(fmt.Sprintf("        %d:\n", i))
			b.WriteString(fmt.Sprintf("          Labels:%s\n", formatLabelMap(exp.ExpLabels)))
			if len(exp.ExpAnnotations) > 0 {
				b.WriteString(fmt.Sprintf("          Annotations:%s\n", formatLabelMap(exp.ExpAnnotations)))
			}
		}
		b.WriteString("        ], \n")
	}

	// Format actual alerts
	b.WriteString("    got:")
	if len(actual) == 0 {
		b.WriteString("[]")
	} else {
		b.WriteString("[\n")
		for i, act := range actual {
			b.WriteString(fmt.Sprintf("        %d:\n", i))
			b.WriteString(fmt.Sprintf("          Labels:%s\n", act.Labels.String()))
			if !act.Annotations.IsEmpty() {
				b.WriteString(fmt.Sprintf("          Annotations:%s\n", act.Annotations.String()))
			}
		}
		b.WriteString("        ]")
	}

	return b.String()
}

// formatLabelMap formats a map of labels in promtool style: {key1="value1", key2="value2"}
func formatLabelMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("{")
	first := true
	for _, k := range keys {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(fmt.Sprintf("%s=%q", k, m[k]))
	}
	b.WriteString("}")
	return b.String()
}

// formatVectorDiff formats a diff between expected and actual vector samples in promtool style.
func (ta *testAssertion) formatVectorDiff(expected []sample, actual promql.Vector) string {
	var b strings.Builder

	// Format expected samples
	b.WriteString("\n    exp:[\n")
	if len(expected) == 0 {
		b.WriteString("        ]\n")
	} else {
		for i, exp := range expected {
			b.WriteString(fmt.Sprintf("        %d:\n", i))
			b.WriteString(fmt.Sprintf("          Labels:%s\n", exp.Labels))
			b.WriteString(fmt.Sprintf("          Value:%v\n", exp.Value))
		}
		b.WriteString("        ], \n")
	}

	// Format actual samples
	b.WriteString("    got:")
	if len(actual) == 0 {
		b.WriteString("[]")
	} else {
		b.WriteString("[\n")
		for i, act := range actual {
			b.WriteString(fmt.Sprintf("        %d:\n", i))
			b.WriteString(fmt.Sprintf("          Labels:%s\n", act.Metric.String()))
			b.WriteString(fmt.Sprintf("          Value:%v\n", act.F))
		}
		b.WriteString("        ]")
	}

	return b.String()
}

// Sorting functions

func sortExpectedAlerts(alerts []alert) []alert {
	sorted := make([]alert, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		return compareAlertMaps(sorted[i].ExpLabels, sorted[j].ExpLabels)
	})
	return sorted
}

func sortActiveAlerts(alerts []*activeAlert) []*activeAlert {
	sorted := make([]*activeAlert, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		return labels.Compare(sorted[i].Labels, sorted[j].Labels) < 0
	})
	return sorted
}

func sortLogLines(logs []logLineResult) {
	sort.Slice(logs, func(i, j int) bool {
		cmp := labels.Compare(logs[i].Labels, logs[j].Labels)
		if cmp != 0 {
			return cmp < 0
		}
		return logs[i].Timestamp < logs[j].Timestamp
	})
}

func compareAlertMaps(a, b map[string]string) bool {
	// Compare lexicographically by converting to strings
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return aStr < bStr
}

// ParseLogQLExpr parses a LogQL expression and returns its type.
func ParseLogQLExpr(expr string) (syntax.Expr, error) {
	return syntax.ParseExpr(expr)
}

// IsLogQuery determines if a LogQL expression returns log streams (vs metrics).
func IsLogQuery(expr syntax.Expr) bool {
	switch expr.(type) {
	case syntax.LogSelectorExpr:
		return true
	case syntax.SampleExpr:
		return false
	default:
		return false
	}
}
