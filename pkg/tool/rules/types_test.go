package rules

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestParseTestFile(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yml")

	testYAML := `
rule_files:
  - rules.yml

evaluation_interval: 1m

tests:
  - name: "Test alert fires"
    interval: 1m
    input_streams:
      - labels: '{job="test", instance="localhost"}'
        lines:
          - 'log line 1'
          - 'log line 2'
          - 'log line 3'
          - 'log line 4'
          - 'log line 5'
      - labels: '{job="test"}'
        lines:
          - 'log 1'
          - 'log 2'
          - 'log 3'
          - 'log 4'
          - 'log 5'
          - 'log 6'
          - 'log 7'
          - 'log 8'
          - 'log 9'
          - 'log 10'

    alert_rule_test:
      - eval_time: 5m
        alertname: InstanceDown
        exp_alerts:
          - exp_labels:
              job: test
              instance: localhost
            exp_annotations:
              summary: "Instance is down"

    logql_expr_test:
      - expr: 'count_over_time({job="test"}[5m])'
        eval_time: 5m
        exp_samples:
          - labels: '{}'
            value: 42
`

	err := os.WriteFile(testFile, []byte(testYAML), 0644)
	require.NoError(t, err)

	// Create a dummy rules file so glob doesn't fail
	rulesFile := filepath.Join(tmpDir, "rules.yml")
	err = os.WriteFile(rulesFile, []byte("groups: []"), 0644)
	require.NoError(t, err)

	// Parse the test file
	utf, err := parseTestFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, utf)

	// Validate basic fields
	require.Equal(t, 1, len(utf.RuleFiles))
	require.Equal(t, filepath.Join(tmpDir, "rules.yml"), utf.RuleFiles[0])
	require.Equal(t, model.Duration(1*time.Minute), utf.EvaluationInterval)
	require.Equal(t, 1, len(utf.Tests))

	// Validate test group
	test := utf.Tests[0]
	require.Equal(t, "Test alert fires", test.TestGroupName)
	require.Equal(t, model.Duration(1*time.Minute), test.Interval)
	require.Equal(t, 2, len(test.InputStreams))
	require.Equal(t, 1, len(test.AlertRuleTests))
	require.Equal(t, 1, len(test.LogQLExprTests))

	// Validate input streams
	require.Equal(t, `{job="test", instance="localhost"}`, test.InputStreams[0].Labels)
	require.Equal(t, []string{"log line 1", "log line 2", "log line 3", "log line 4", "log line 5"}, test.InputStreams[0].Lines)

	// Validate alert test
	alertTest := test.AlertRuleTests[0]
	require.Equal(t, model.Duration(5*time.Minute), alertTest.EvalTime)
	require.Equal(t, "InstanceDown", alertTest.Alertname)
	require.Equal(t, 1, len(alertTest.ExpAlerts))
	require.Equal(t, "test", alertTest.ExpAlerts[0].ExpLabels["job"])
	require.Equal(t, "Instance is down", alertTest.ExpAlerts[0].ExpAnnotations["summary"])

	// Validate LogQL expression test
	exprTest := test.LogQLExprTests[0]
	require.Equal(t, `count_over_time({job="test"}[5m])`, exprTest.Expr)
	require.Equal(t, model.Duration(5*time.Minute), exprTest.EvalTime)
	require.Equal(t, 1, len(exprTest.ExpSamples))
	require.Equal(t, float64(42), exprTest.ExpSamples[0].Value)
}

func TestResolveAndGlobFilepaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some test rule files
	err := os.WriteFile(filepath.Join(tmpDir, "rules1.yml"), []byte(""), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "rules2.yml"), []byte(""), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "alerts.yml"), []byte(""), 0644)
	require.NoError(t, err)

	tests := []struct {
		name          string
		inputFiles    []string
		expectedCount int
		expectError   bool
	}{
		{
			name:          "absolute path",
			inputFiles:    []string{filepath.Join(tmpDir, "rules1.yml")},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:          "relative path",
			inputFiles:    []string{"rules1.yml"},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:          "glob pattern",
			inputFiles:    []string{"rules*.yml"},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:          "wildcard all yml",
			inputFiles:    []string{"*.yml"},
			expectedCount: 3,
			expectError:   false,
		},
		{
			name:          "no match",
			inputFiles:    []string{"nonexistent*.yml"},
			expectedCount: 0,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utf := &unitTestFile{
				RuleFiles: tt.inputFiles,
			}

			err := resolveAndGlobFilepaths(tmpDir, utf)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedCount, len(utf.RuleFiles))
			}
		})
	}
}

func TestValidateTestFile(t *testing.T) {
	tests := []struct {
		name        string
		testFile    *unitTestFile
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid test file",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						TestGroupName: "test1",
						InputStreams:   []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						AlertRuleTests: []alertTestCase{
							{Alertname: "TestAlert", EvalTime: 0},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "no rule files",
			testFile: &unitTestFile{
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						AlertRuleTests: []alertTestCase{
							{Alertname: "TestAlert", EvalTime: 0},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "no rule files specified",
		},
		{
			name: "no tests",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests:     []testGroup{},
			},
			expectError: true,
			errorMsg:    "no tests specified",
		},
		{
			name: "no input series",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						AlertRuleTests: []alertTestCase{
							{Alertname: "TestAlert", EvalTime: 0},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "no input streams specified",
		},
		{
			name: "no tests in group",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
					},
				},
			},
			expectError: true,
			errorMsg:    "no alert or expression tests specified",
		},
		{
			name: "duplicate group eval order",
			testFile: &unitTestFile{
				RuleFiles:      []string{"rules.yml"},
				GroupEvalOrder: []string{"group1", "group2", "group1"},
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						AlertRuleTests: []alertTestCase{
							{Alertname: "TestAlert", EvalTime: 0},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "duplicate group name in eval order: group1",
		},
		{
			name: "alert test missing alertname",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						AlertRuleTests: []alertTestCase{
							{EvalTime: 0},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "alert test #0 missing alertname",
		},
		{
			name: "expression test missing expr",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						LogQLExprTests: []logqlTestCase{
							{EvalTime: 0, ExpSamples: []sample{{Value: 1}}},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "expression test #0 missing expr",
		},
		{
			name: "expression test no expected results",
			testFile: &unitTestFile{
				RuleFiles: []string{"rules.yml"},
				Tests: []testGroup{
					{
						InputStreams: []stream{{Labels: "up", Lines: []string{"log line 1"}}},
						LogQLExprTests: []logqlTestCase{
							{Expr: "up", EvalTime: 0},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "expression test #0 has no expected samples or logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFile.validate()
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
