package rules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestRunUnitTests_AlertRules(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a rule file
	ruleContent := `
groups:
  - name: test_alerts
    interval: 1m
    rules:
      - alert: HighCount
        expr: 'count_over_time({job="test"}[5m]) > 2'
        labels:
          severity: warning
`
	ruleFile := filepath.Join(tmpDir, "rules.yml")
	err := os.WriteFile(ruleFile, []byte(ruleContent), 0644)
	require.NoError(t, err)

	// Create a test file
	testContent := `
rule_files:
  - ` + ruleFile + `

evaluation_interval: 1m

tests:
  - name: "Test high count alert"
    interval: 1m
    input_streams:
      - labels: '{job="test"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'
          - 'log line 4'
          - 'log line 5'
          - 'log line 6'

    alert_rule_test:
      - alertname: HighCount
        eval_time: 3m
        exp_alerts:
          - exp_labels:
              severity: warning
              job: test
`
	testFile := filepath.Join(tmpDir, "test.yml")
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Run tests
	err = RunUnitTests([]string{testFile}, log.NewNopLogger())
	require.NoError(t, err)
}

func TestRunUnitTests_LogQLExpr(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a rule file (required even if empty)
	ruleContent := `
groups:
  - name: empty_group
    interval: 1m
    rules: []
`
	ruleFile := filepath.Join(tmpDir, "rules.yml")
	err := os.WriteFile(ruleFile, []byte(ruleContent), 0644)
	require.NoError(t, err)

	// Create a test file with LogQL expression test
	testContent := `
rule_files:
  - ` + ruleFile + `

evaluation_interval: 1m

tests:
  - name: "Test LogQL count query"
    interval: 1m
    input_streams:
      - labels: '{job="test"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'
          - 'log line 4'
          - 'log line 5'

    logql_expr_test:
      - expr: 'count_over_time({job="test"}[5m])'
        eval_time: 4m
        exp_samples:
          - labels: '{job="test"}'
            value: 5
`
	testFile := filepath.Join(tmpDir, "test.yml")
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Run tests
	err = RunUnitTests([]string{testFile}, log.NewNopLogger())
	require.NoError(t, err)
}

func TestRunUnitTests_FailingTest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a rule file
	ruleContent := `
groups:
  - name: test_alerts
    interval: 1m
    rules:
      - alert: ShouldNotFire
        expr: 'count_over_time({job="test"}[1m]) > 100'
`
	ruleFile := filepath.Join(tmpDir, "rules.yml")
	err := os.WriteFile(ruleFile, []byte(ruleContent), 0644)
	require.NoError(t, err)

	// Create a test file that expects an alert that won't fire
	testContent := `
rule_files:
  - ` + ruleFile + `

evaluation_interval: 1m

tests:
  - name: "Test that should fail"
    interval: 1m
    input_streams:
      - labels: '{job="test"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'

    alert_rule_test:
      - alertname: ShouldNotFire
        eval_time: 3m
        exp_alerts:
          - exp_labels:
              job: test
`
	testFile := filepath.Join(tmpDir, "test.yml")
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Run tests - should fail
	err = RunUnitTests([]string{testFile}, log.NewNopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "tests failed")
}

func TestTestRunner_RunAlertTest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create rule file
	ruleContent := `
groups:
  - name: test_group
    interval: 1m
    rules:
      - alert: TestAlert
        expr: 'count_over_time({job="test"}[1m]) > 0'
`
	ruleFile := filepath.Join(tmpDir, "rules.yml")
	err := os.WriteFile(ruleFile, []byte(ruleContent), 0644)
	require.NoError(t, err)

	// Setup evaluator
	storage := newTestStorage()
	evaluator := newTestEvaluator(storage, log.NewNopLogger())

	err = evaluator.loadRules(
		[]string{ruleFile},
		model.Duration(1*time.Minute),
		labels.EmptyLabels(),
		"",
	)
	require.NoError(t, err)

	// Create test runner
	runner := newTestRunner(evaluator, log.NewNopLogger())

	// Create test group
	testGroup := &testGroup{
		TestGroupName: "test",
		Interval:      model.Duration(1 * time.Minute),
		InputStreams: []stream{
			{
				Labels: `{job="test"}`,
				Lines:  []string{"log line 1", "log line 2", "log line 3"},
			},
		},
	}

	// Load input streams
	err = storage.parseAndLoadStreams(testGroup.InputStreams, testGroup.Interval)
	require.NoError(t, err)

	// Create alert test
	alertTest := &alertTestCase{
		Alertname: "TestAlert",
		EvalTime:  model.Duration(2 * time.Minute),
		ExpAlerts: []alert{
			{
				ExpLabels: map[string]string{
					"job": "test",
				},
			},
		},
	}

	// Run test
	result := runner.runAlertTest(alertTest, testGroup)
	if !result.Passed {
		t.Logf("Test failed with error: %v", result.Error)
		t.Logf("Result: %+v", result)
	}
	require.True(t, result.Passed, "test should pass")
	require.NoError(t, result.Error)
}

func TestTestRunner_RunLogQLTest(t *testing.T) {
	// Setup evaluator
	storage := newTestStorage()
	evaluator := newTestEvaluator(storage, log.NewNopLogger())

	// Create test runner
	runner := newTestRunner(evaluator, log.NewNopLogger())

	// Create test group
	testGroup := &testGroup{
		TestGroupName: "test",
		Interval:      model.Duration(1 * time.Minute),
		InputStreams: []stream{
			{
				Labels: `{job="test"}`,
				Lines:  []string{"log line 1", "log line 2", "log line 3", "log line 4", "log line 5"},
			},
		},
	}

	// Load input streams
	err := storage.parseAndLoadStreams(testGroup.InputStreams, testGroup.Interval)
	require.NoError(t, err)

	// Create LogQL test
	logqlTest := &logqlTestCase{
		Expr:     `count_over_time({job="test"}[5m])`,
		EvalTime: model.Duration(5 * time.Minute),
		ExpSamples: []sample{
			{
				Labels: `{job="test"}`,
				Value:  5,
			},
		},
	}

	// Run test
	result := runner.runLogQLTest(logqlTest, testGroup)
	require.True(t, result.Passed, "test should pass")
	require.NoError(t, result.Error)
}

func TestTestRunner_MultipleTestGroups(t *testing.T) {
	tmpDir := t.TempDir()

	// Create rule file
	ruleContent := `
groups:
  - name: alerts
    interval: 1m
    rules:
      - alert: Alert1
        expr: 'count_over_time({job="app1"}[1m]) > 2'

      - alert: Alert2
        expr: 'count_over_time({job="app2"}[1m]) > 1'
`
	ruleFile := filepath.Join(tmpDir, "rules.yml")
	err := os.WriteFile(ruleFile, []byte(ruleContent), 0644)
	require.NoError(t, err)

	// Create test file with multiple test groups
	testContent := `
rule_files:
  - ` + ruleFile + `

evaluation_interval: 1m

tests:
  - name: "Test group 1"
    interval: 1m
    input_streams:
      - labels: '{job="app1"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'
          - 'log line 4'
          - 'log line 5'

    alert_rule_test:
      - alertname: Alert1
        eval_time: 4m
        exp_alerts:
          - exp_labels:
              job: app1

  - name: "Test group 2"
    interval: 1m
    input_streams:
      - labels: '{job="app2"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'
          - 'log line 4'
          - 'log line 5'

    alert_rule_test:
      - alertname: Alert2
        eval_time: 3m
        exp_alerts:
          - exp_labels:
              job: app2
`
	testFile := filepath.Join(tmpDir, "test.yml")
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Run tests
	err = RunUnitTests([]string{testFile}, log.NewNopLogger())
	require.NoError(t, err)
}

func TestExecuteLogQLQuery(t *testing.T) {
	// Setup storage and evaluator
	storage := newTestStorage()
	evaluator := newTestEvaluator(storage, log.NewNopLogger())

	// Load test data
	inputStreams := []stream{
		{
			Labels: `{job="test", level="error"}`,
			Lines:  []string{"log line 1", "log line 2", "log line 3", "log line 4", "log line 5"},
		},
	}
	err := storage.parseAndLoadStreams(inputStreams, model.Duration(1*time.Minute))
	require.NoError(t, err)

	// Execute query
	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	evalTime := time.Unix(0, 0).UTC().Add(5 * time.Minute)

	result, err := evaluator.executeLogQLQuery(ctx, `count_over_time({job="test"}[5m])`, evalTime)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check result type
	_, ok := result.Data.(promql.Vector)
	require.True(t, ok, "result should be a Vector")
}
