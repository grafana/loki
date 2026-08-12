package rules

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/push"
)

func TestCompareAlerts(t *testing.T) {
	ta := newTestAssertion()

	tests := []struct {
		name        string
		testCase    alertTestCase
		actual      []*activeAlert
		expectError bool
	}{
		{
			name: "matching alerts",
			testCase: alertTestCase{
				Alertname: "TestAlert",
				EvalTime:  model.Duration(5 * time.Minute),
				ExpAlerts: []alert{
					{
						ExpLabels: map[string]string{
							"severity": "warning",
							"job":      "test",
						},
						ExpAnnotations: map[string]string{
							"summary": "Test alert",
						},
					},
				},
			},
			actual: []*activeAlert{
				{
					Labels:      labels.FromStrings("job", "test", "severity", "warning"),
					Annotations: labels.FromStrings("summary", "Test alert"),
					Value:       1.0,
				},
			},
			expectError: false,
		},
		{
			name: "alert count mismatch",
			testCase: alertTestCase{
				Alertname: "TestAlert",
				EvalTime:  model.Duration(5 * time.Minute),
				ExpAlerts: []alert{
					{
						ExpLabels: map[string]string{
							"severity": "warning",
						},
					},
				},
			},
			actual: []*activeAlert{
				{
					Labels: labels.FromStrings("severity", "warning"),
				},
				{
					Labels: labels.FromStrings("severity", "critical"),
				},
			},
			expectError: true,
		},
		{
			name: "empty alerts match",
			testCase: alertTestCase{
				Alertname: "TestAlert",
				EvalTime:  model.Duration(5 * time.Minute),
				ExpAlerts: []alert{},
			},
			actual:      []*activeAlert{},
			expectError: false,
		},
		{
			name: "label mismatch",
			testCase: alertTestCase{
				Alertname: "TestAlert",
				EvalTime:  model.Duration(5 * time.Minute),
				ExpAlerts: []alert{
					{
						ExpLabels: map[string]string{
							"severity": "warning",
						},
					},
				},
			},
			actual: []*activeAlert{
				{
					Labels: labels.FromStrings("severity", "critical"),
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ta.compareAlerts(tt.testCase, tt.actual)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompareVector(t *testing.T) {
	ta := newTestAssertion()

	tests := []struct {
		name        string
		testCase    logqlTestCase
		vector      promql.Vector
		expectError bool
	}{
		{
			name: "matching vector",
			testCase: logqlTestCase{
				Expr:     `rate({job="test"}[5m])`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{
						Labels: `{job="test"}`,
						Value:  1.5,
					},
				},
			},
			vector: promql.Vector{
				{
					Metric: labels.FromStrings("job", "test"),
					T:      5 * 60 * 1000,
					F:      1.5,
				},
			},
			expectError: false,
		},
		{
			name: "value mismatch",
			testCase: logqlTestCase{
				Expr:     `rate({job="test"}[5m])`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{
						Labels: `{job="test"}`,
						Value:  1.5,
					},
				},
			},
			vector: promql.Vector{
				{
					Metric: labels.FromStrings("job", "test"),
					T:      5 * 60 * 1000,
					F:      2.0,
				},
			},
			expectError: true,
		},
		{
			name: "count mismatch",
			testCase: logqlTestCase{
				Expr:     `rate({job="test"}[5m])`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{
						Labels: `{job="test"}`,
						Value:  1.5,
					},
				},
			},
			vector: promql.Vector{
				{
					Metric: labels.FromStrings("job", "test"),
					F:      1.5,
				},
				{
					Metric: labels.FromStrings("job", "other"),
					F:      2.0,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ta.compareVector(tt.testCase, tt.vector)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompareScalar(t *testing.T) {
	ta := newTestAssertion()

	tests := []struct {
		name        string
		testCase    logqlTestCase
		scalar      promql.Scalar
		expectError bool
	}{
		{
			name: "matching scalar",
			testCase: logqlTestCase{
				Expr:     `1 + 1`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{
						Value: 2.0,
					},
				},
			},
			scalar: promql.Scalar{
				T: 5 * 60 * 1000,
				V: 2.0,
			},
			expectError: false,
		},
		{
			name: "value mismatch",
			testCase: logqlTestCase{
				Expr:     `1 + 1`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{
						Value: 2.0,
					},
				},
			},
			scalar: promql.Scalar{
				T: 5 * 60 * 1000,
				V: 3.0,
			},
			expectError: true,
		},
		{
			name: "wrong number of expected samples",
			testCase: logqlTestCase{
				Expr:     `1 + 1`,
				EvalTime: model.Duration(5 * time.Minute),
				ExpSamples: []sample{
					{Value: 2.0},
					{Value: 3.0},
				},
			},
			scalar: promql.Scalar{
				V: 2.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ta.compareScalar(tt.testCase, tt.scalar)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompareFloat(t *testing.T) {
	tests := []struct {
		name        string
		expected    float64
		actual      float64
		expectError bool
	}{
		{
			name:        "exact match",
			expected:    1.5,
			actual:      1.5,
			expectError: false,
		},
		{
			name:        "exact mismatch",
			expected:    1.5,
			actual:      1.6,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ta := newTestAssertion()
			err := ta.compareFloat(tt.expected, tt.actual)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompareLogQLSamples(t *testing.T) {
	ta := newTestAssertion()

	t.Run("vector result", func(t *testing.T) {
		testCase := logqlTestCase{
			Expr:     `rate({job="test"}[5m])`,
			EvalTime: model.Duration(5 * time.Minute),
			ExpSamples: []sample{
				{
					Labels: `{job="test"}`,
					Value:  1.5,
				},
			},
		}

		result := &logqlmodel.Result{
			Data: promql.Vector{
				{
					Metric: labels.FromStrings("job", "test"),
					F:      1.5,
				},
			},
		}

		err := ta.compareLogQLSamples(testCase, result)
		require.NoError(t, err)
	})

	t.Run("scalar result", func(t *testing.T) {
		testCase := logqlTestCase{
			Expr:     `1 + 1`,
			EvalTime: model.Duration(5 * time.Minute),
			ExpSamples: []sample{
				{Value: 2.0},
			},
		}

		result := &logqlmodel.Result{
			Data: promql.Scalar{V: 2.0},
		}

		err := ta.compareLogQLSamples(testCase, result)
		require.NoError(t, err)
	})
}

func TestCompareLogQLLogs(t *testing.T) {
	ta := newTestAssertion()

	testCase := logqlTestCase{
		Expr:     `{job="test"}`,
		EvalTime: model.Duration(5 * time.Minute),
		ExpLogs: []logLine{
			{
				Labels: `{job="test"}`,
				Line:   "log line 1",
			},
		},
	}

	result := &logqlmodel.Result{
		Data: logqlmodel.Streams{
			{
				Labels: `{job="test"}`,
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(0, 0),
						Line:      "log line 1",
					},
				},
			},
		},
	}

	err := ta.compareLogQLLogs(testCase, result)
	require.NoError(t, err)
}

func TestSortExpectedAlerts(t *testing.T) {
	alerts := []alert{
		{
			ExpLabels: map[string]string{
				"severity": "critical",
			},
		},
		{
			ExpLabels: map[string]string{
				"severity": "warning",
			},
		},
	}

	sorted := sortExpectedAlerts(alerts)
	require.Equal(t, 2, len(sorted))
	// Should be sorted in some consistent order
}

func TestSortActiveAlerts(t *testing.T) {
	alerts := []*activeAlert{
		{
			Labels: labels.FromStrings("severity", "critical"),
		},
		{
			Labels: labels.FromStrings("severity", "warning"),
		},
	}

	sorted := sortActiveAlerts(alerts)
	require.Equal(t, 2, len(sorted))
	// Should be sorted by labels
	require.Equal(t, "critical", sorted[0].Labels.Get("severity"))
	require.Equal(t, "warning", sorted[1].Labels.Get("severity"))
}

func TestFormatAlertDiff(t *testing.T) {
	ta := newTestAssertion()

	expected := []alert{
		{
			ExpLabels: map[string]string{
				"severity": "warning",
			},
		},
	}

	actual := []*activeAlert{
		{
			Labels: labels.FromStrings("severity", "critical"),
			Value:  1.5,
		},
	}

	diff := ta.formatAlertDiff(expected, actual)
	require.Contains(t, diff, "exp:[")
	require.Contains(t, diff, "got:")
	require.Contains(t, diff, "warning")
	require.Contains(t, diff, "critical")
}

func TestFormatVectorDiff(t *testing.T) {
	ta := newTestAssertion()

	expected := []sample{
		{
			Labels: `{job="test"}`,
			Value:  1.5,
		},
	}

	actual := promql.Vector{
		{
			Metric: labels.FromStrings("job", "test"),
			F:      2.0,
		},
	}

	diff := ta.formatVectorDiff(expected, actual)
	require.Contains(t, diff, "exp:[")
	require.Contains(t, diff, "got:")
	require.Contains(t, diff, "1.5")
	require.Contains(t, diff, "2")
}
