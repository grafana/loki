package rules

import (
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gotest.tools/assert"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

func TestAggregateBy(t *testing.T) {
	tt := []struct {
		name            string
		rn              RuleNamespace
		applyTo         func(group rwrulefmt.RuleGroup, rule rulefmt.RuleNode) bool
		expectedExpr    []string
		count, modified int
		expect          error
	}{
		{
			name:  "with no rules",
			rn:    RuleNamespace{},
			count: 0, modified: 0, expect: nil,
		},
		{
			name: "no modification",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "WithoutAggregation", Rules: []rulefmt.RuleNode{
								{Alert: yaml.Node{Value: "WithoutAggregation"}, Expr: yaml.Node{Value: "up != 1"}},
							},
						},
					},
				},
			},
			expectedExpr: []string{"up != 1"},
			count:        1, modified: 0, expect: nil,
		},
		{
			name: "no change in the query but lints with 'without' in the aggregation",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "SkipWithout",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{Value: "SkipWithout"},
									Expr: yaml.Node{
										Value: `
											min without (alertmanager) (
												rate(prometheus_notifications_errors_total{job="default/prometheus"}[5m])
											/
												rate(prometheus_notifications_sent_total{job="default/prometheus"}[5m])
											)
											* 100
											> 3`,
									},
								},
							},
						},
					},
				},
			},
			expectedExpr: []string{`min without (alertmanager) (rate(prometheus_notifications_errors_total{job="default/prometheus"}[5m]) / rate(prometheus_notifications_sent_total{job="default/prometheus"}[5m])) * 100 > 3`},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with an aggregation modification",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "WithAggregation",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{Value: "WithAggregation"},
									Expr: yaml.Node{
										Value: `
										sum(rate(cortex_prometheus_rule_evaluation_failures_total[1m])) by (namespace, job)
										/
										sum(rate(cortex_prometheus_rule_evaluations_total[1m])) by (namespace, job)
										> 0.01`,
									},
								},
							},
						},
					},
				},
			},
			expectedExpr: []string{"sum by (namespace, job, cluster) (rate(cortex_prometheus_rule_evaluation_failures_total[1m])) / sum by (namespace, job, cluster) (rate(cortex_prometheus_rule_evaluations_total[1m])) > 0.01"},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with 'count' as the aggregation",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "CountAggregation",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{
										Value: "CountAggregation",
									},
									Expr: yaml.Node{
										Value: `
										count(count by (gitVersion) (label_replace(kubernetes_build_info{job!~"kube-dns|coredns"},"gitVersion","$1","gitVersion","(v[0-9]*.[0-9]*.[0-9]*).*"))) > 1`,
									},
								},
							},
						},
					},
				},
			},
			expectedExpr: []string{`count by (cluster) (count by (gitVersion, cluster) (label_replace(kubernetes_build_info{job!~"kube-dns|coredns"}, "gitVersion", "$1", "gitVersion", "(v[0-9]*.[0-9]*.[0-9]*).*"))) > 1`},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with vector matching in binary operations",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "BinaryExpressions",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{Value: "VectorMatching"},
									Expr:  yaml.Node{Value: `count by (cluster, node) (sum by (node, cpu, cluster) (node_cpu_seconds_total{job="default/node-exporter"} * on (namespace, instance) group_left (node) node_namespace_pod:kube_pod_info:))`},
								},
							},
						},
					},
				},
			},
			expectedExpr: []string{`count by (cluster, node) (sum by (node, cpu, cluster) (node_cpu_seconds_total{job="default/node-exporter"} * on (namespace, instance, cluster) group_left (node) node_namespace_pod:kube_pod_info:))`},
			count:        1, modified: 1, expect: nil,
		},
		{
			name: "with a query skipped",
			rn: RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Name: "CountAggregation",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{
										Value: "CountAggregation",
									},
									Expr: yaml.Node{
										Value: `count by (namespace) (test_series) > 1`,
									},
								},
							},
						},
					}, {
						RuleGroup: rulefmt.RuleGroup{
							Name: "CountSkipped",
							Rules: []rulefmt.RuleNode{
								{
									Alert: yaml.Node{
										Value: "CountSkipped",
									},
									Expr: yaml.Node{
										Value: `count by (namespace) (test_series) > 1`,
									},
								},
							},
						},
					},
				},
			},
			applyTo: func(group rwrulefmt.RuleGroup, _ rulefmt.RuleNode) bool {
				return group.Name != "CountSkipped"
			},
			expectedExpr: []string{`count by (namespace, cluster) (test_series) > 1`, `count by (namespace) (test_series) > 1`},
			count:        2, modified: 1, expect: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			c, m, err := tc.rn.AggregateBy("cluster", tc.applyTo)

			require.Equal(t, tc.expect, err)
			assert.Equal(t, tc.count, c)
			assert.Equal(t, tc.modified, m)

			// Only verify the PromQL expression if it has been modified
			expectedIdx := 0
			for _, g := range tc.rn.Groups {
				for _, r := range g.Rules {
					require.Equal(t, tc.expectedExpr[expectedIdx], r.Expr.Value)
					expectedIdx++
				}
			}
		})
	}
}

func TestLintExpressions(t *testing.T) {
	tt := []struct {
		name            string
		expr            string
		expected        string
		err             string
		count, modified int
		logql           bool
	}{
		{
			name:     "logql simple",
			expr:     `count_over_time({ foo = "bar" }[12m]) > 1`,
			expected: `(count_over_time({foo="bar"}[12m]) > 1)`,
			count:    1, modified: 1,
			logql: true,
		},
		{
			name: "logql v2",
			expr: `sum by (org_id) (
  sum_over_time(
  {job="loki-prod/query-frontend"}
      |= "metrics.go"
      | logfmt
      | unwrap duration(duration) [1m])
   )
`,
			expected: `sum by (org_id)(sum_over_time({job="loki-prod/query-frontend"} |= "metrics.go" | logfmt | unwrap duration(duration)[1m]))`,
			count:    1, modified: 1,
			logql: true,
		},
		{
			name:  "logql badExpr",
			expr:  `count_over_time({ foo != "bar"%LKJ }[12m]) >         1`,
			count: 0, modified: 0,
			logql: true,
			err:   "parse error at line 1, col 31: syntax error: unexpected %, expecting } or ,",
		},
		{
			name:     "logql vector expression",
			expr:     `count(count_over_time({foo="bar"}[1m])) or vector(1)`,
			expected: `(count(count_over_time({foo="bar"}[1m])) or vector(1.000000))`,
			count:    1, modified: 1,
			logql: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := RuleNamespace{Groups: []rwrulefmt.RuleGroup{
				{
					RuleGroup: rulefmt.RuleGroup{
						Rules: []rulefmt.RuleNode{
							{
								Alert: yaml.Node{Value: "AName"},
								Expr:  yaml.Node{Value: tc.expr},
							},
						},
					},
				},
			},
			}

			c, m, err := r.LintExpressions()
			rexpr := r.Groups[0].Rules[0].Expr.Value

			require.Equal(t, tc.count, c)
			require.Equal(t, tc.modified, m)
			if err == nil {
				require.Equal(t, tc.expected, rexpr)
			}

			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err)
			}
		})
	}
}

func TestCheckRecordingRules(t *testing.T) {
	tt := []struct {
		name     string
		ruleName string
		count    int
		strict   bool
	}{
		{
			name:     "follows rule name conventions",
			ruleName: "level:metric:operation",
			count:    0,
		},
		{
			name:     "doesn't follow rule name conventions",
			ruleName: "level_metric_operation",
			count:    1,
		},
		{
			name:     "almost follows rule name conventions",
			ruleName: "level:metric_operation",
			count:    1,
			strict:   true,
		},
		{
			name:     "almost follows rule name conventions",
			ruleName: "level:metric_operation",
			count:    0,
		},
		{
			name:     "follows rule name conventions extra",
			ruleName: "level:metric:something_else:operation",
			count:    0,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := RuleNamespace{
				Groups: []rwrulefmt.RuleGroup{
					{
						RuleGroup: rulefmt.RuleGroup{
							Rules: []rulefmt.RuleNode{
								{
									Record: yaml.Node{Value: tc.ruleName},
									Expr:   yaml.Node{Value: "rate(some_metric_total)[5m]"}},
							},
						},
					},
				},
			}

			n := r.CheckRecordingRules(tc.strict)
			require.Equal(t, tc.count, n, "failed rule: %s", tc.ruleName)
		})
	}
}
