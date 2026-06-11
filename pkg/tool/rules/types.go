package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"gopkg.in/yaml.v3"
)

// unitTestFile holds the contents of a single unit test file.
// This structure mirrors promtool's test file format for familiarity.
type unitTestFile struct {
	// RuleFiles contains the list of rule files to test
	RuleFiles []string `yaml:"rule_files"`

	// EvaluationInterval is the default evaluation interval for rules
	EvaluationInterval model.Duration `yaml:"evaluation_interval,omitempty"`

	// GroupEvalOrder specifies the order in which rule groups should be evaluated
	GroupEvalOrder []string `yaml:"group_eval_order,omitempty"`

	// Tests contains the individual test cases
	Tests []testGroup `yaml:"tests"`
}

// testGroup represents a group of input streams and tests associated with it.
type testGroup struct {
	// Interval is the evaluation interval for this test group
	Interval model.Duration `yaml:"interval,omitempty"`

	// InputStreams contains the log streams to inject for testing
	InputStreams []stream `yaml:"input_streams"`

	// AlertRuleTests contains tests for alert rules
	AlertRuleTests []alertTestCase `yaml:"alert_rule_test,omitempty"`

	// LogQLExprTests contains tests for LogQL expressions
	LogQLExprTests []logqlTestCase `yaml:"logql_expr_test,omitempty"`

	// ExternalLabels to set for this test group
	ExternalLabels labels.Labels `yaml:"external_labels,omitempty"`

	// ExternalURL to set for this test group
	ExternalURL string `yaml:"external_url,omitempty"`

	// TestGroupName is the name of this test group
	TestGroupName string `yaml:"name,omitempty"`
}

// stream represents a Loki log stream for testing.
type stream struct {
	// Labels is the LogQL label selector (e.g., '{job="test", level="error"}')
	// Only equality matchers (=) are supported, not regex matchers (=~, !=, !~)
	Labels string `yaml:"labels"`

	// StructuredMetadata contains key-value pairs attached to all log entries in this stream
	// Use for high-cardinality data like trace IDs, user IDs, request IDs
	// Optional: if not specified, entries will have no structured metadata
	StructuredMetadata map[string]string `yaml:"structured_metadata,omitempty"`

	// Lines contains explicit log line content for testing log parsing and filtering
	// Use for testing LogQL features like | json, |=, |~, etc.
	Lines []string `yaml:"lines,omitempty"`
}

// Validate checks that the stream has lines.
func (s *stream) Validate() error {
	if len(s.Lines) == 0 {
		return fmt.Errorf("stream %q must have 'lines'", s.Labels)
	}

	return nil
}

// alertTestCase represents a test case for alert rules.
type alertTestCase struct {
	// EvalTime is the time at which to evaluate the alert
	EvalTime model.Duration `yaml:"eval_time"`

	// Alertname is the name of the alert to check
	Alertname string `yaml:"alertname"`

	// ExpAlerts are the expected alerts at this eval time
	ExpAlerts []alert `yaml:"exp_alerts"`
}

// alert represents an expected alert with its labels and annotations.
type alert struct {
	// ExpLabels are the expected labels on the alert
	ExpLabels map[string]string `yaml:"exp_labels"`

	// ExpAnnotations are the expected annotations on the alert
	ExpAnnotations map[string]string `yaml:"exp_annotations"`
}

// logqlTestCase represents a test case for LogQL expressions.
type logqlTestCase struct {
	// Expr is the LogQL expression to evaluate
	Expr string `yaml:"expr"`

	// EvalTime is the time at which to evaluate the expression
	EvalTime model.Duration `yaml:"eval_time"`

	// ExpSamples are the expected samples from the query
	ExpSamples []sample `yaml:"exp_samples,omitempty"`

	// ExpLogs are the expected log lines from the query
	ExpLogs []logLine `yaml:"exp_logs,omitempty"`
}

// sample represents an expected metric sample.
type sample struct {
	// Labels is the label set for this sample (e.g., '{job="test"}')
	Labels string `yaml:"labels"`

	// Value is the expected numeric value
	Value float64 `yaml:"value"`
}

// logLine represents an expected log line.
type logLine struct {
	// Labels is the label set for this log line
	Labels string `yaml:"labels"`

	// Line is the expected log line content
	Line string `yaml:"line"`

	// Timestamp is the timestamp of the log line (nanoseconds)
	Timestamp int64 `yaml:"timestamp,omitempty"`
}

// parseTestFile parses a unit test file from disk.
func parseTestFile(filename string) (*unitTestFile, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file %s: %w", filename, err)
	}

	var unitTestInp unitTestFile
	if err := yaml.Unmarshal(b, &unitTestInp); err != nil {
		return nil, fmt.Errorf("failed to parse test file %s: %w", filename, err)
	}

	// Resolve and glob rule file paths relative to the test file
	if err := resolveAndGlobFilepaths(filepath.Dir(filename), &unitTestInp); err != nil {
		return nil, fmt.Errorf("failed to resolve file paths in %s: %w", filename, err)
	}

	// Set default evaluation interval if not specified
	if unitTestInp.EvaluationInterval == 0 {
		unitTestInp.EvaluationInterval = model.Duration(1 * time.Minute)
	}

	return &unitTestInp, nil
}

// resolveAndGlobFilepaths joins all relative paths in a unit test file
// with a given base directory and replaces all globs with matching files.
func resolveAndGlobFilepaths(baseDir string, utf *unitTestFile) error {
	// First pass: resolve relative paths
	for i, rf := range utf.RuleFiles {
		if rf != "" && !filepath.IsAbs(rf) {
			utf.RuleFiles[i] = filepath.Join(baseDir, rf)
		}
	}

	// Second pass: expand globs
	var globbedFiles []string
	for _, rf := range utf.RuleFiles {
		matches, err := filepath.Glob(rf)
		if err != nil {
			return fmt.Errorf("failed to glob pattern %s: %w", rf, err)
		}

		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "  WARNING: no files match pattern %s\n", rf)
		}

		globbedFiles = append(globbedFiles, matches...)
	}

	utf.RuleFiles = globbedFiles
	return nil
}

// validateTestFile validates that a test file is well-formed.
func (utf *unitTestFile) validate() error {
	if len(utf.RuleFiles) == 0 {
		return fmt.Errorf("no rule files specified")
	}

	if len(utf.Tests) == 0 {
		return fmt.Errorf("no tests specified")
	}

	// Validate group eval order for duplicates
	groupOrderMap := make(map[string]bool)
	for _, gn := range utf.GroupEvalOrder {
		if groupOrderMap[gn] {
			return fmt.Errorf("duplicate group name in eval order: %s", gn)
		}
		groupOrderMap[gn] = true
	}

	// Validate each test group
	for i, test := range utf.Tests {
		testName := test.TestGroupName
		if testName == "" {
			testName = fmt.Sprintf("test #%d", i)
		}

		if len(test.InputStreams) == 0 {
			return fmt.Errorf("%s: no input streams specified", testName)
		}

		if len(test.AlertRuleTests) == 0 && len(test.LogQLExprTests) == 0 {
			return fmt.Errorf("%s: no alert or expression tests specified", testName)
		}

		// Validate alert tests
		for j, alertTest := range test.AlertRuleTests {
			if alertTest.Alertname == "" {
				return fmt.Errorf("%s: alert test #%d missing alertname", testName, j)
			}
		}

		// Validate LogQL expression tests
		for j, exprTest := range test.LogQLExprTests {
			if exprTest.Expr == "" {
				return fmt.Errorf("%s: expression test #%d missing expr", testName, j)
			}
			if len(exprTest.ExpSamples) == 0 && len(exprTest.ExpLogs) == 0 {
				return fmt.Errorf("%s: expression test #%d has no expected samples or logs", testName, j)
			}
		}
	}

	return nil
}
