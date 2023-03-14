package rules_test

import (
	"fmt"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/rules"
	"github.com/stretchr/testify/require"
)

func TestMarshalAlertingRule(t *testing.T) {
	expCfg := `
groups:
  - name: an-alert
    interval: 1m
    limit: 2
    rules:
      - expr: |-
          sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
            /
          sum(rate({app="foo", env="production"}[5m])) by (job)
            > 0.05
        alert: HighPercentageErrors
        for: 10m
        annotations:
          playbook: http://link/to/playbook
          summary: High Percentage Latency
        labels:
          environment: production
          severity: page
      - expr: |-
          sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
            /
          sum(rate({app="foo", env="production"}[5m])) by (job)
            > 0.05
        alert: LowPercentageErrors
        for: 10m
        annotations:
          playbook: http://link/to/playbook
          summary: Low Percentage Latency
        labels:
          environment: production
          severity: low
`

	a := lokiv1.AlertingRule{
		Spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "an-alert",
					Interval: lokiv1.PrometheusDuration("1m"),
					Limit:    2,
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Alert: "HighPercentageErrors",
							Expr: `sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
  /
sum(rate({app="foo", env="production"}[5m])) by (job)
  > 0.05`,
							For: lokiv1.PrometheusDuration("10m"),
							Labels: map[string]string{
								"severity":    "page",
								"environment": "production",
							},
							Annotations: map[string]string{
								"summary":  "High Percentage Latency",
								"playbook": "http://link/to/playbook",
							},
						},
						{
							Alert: "LowPercentageErrors",
							Expr: `sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
  /
sum(rate({app="foo", env="production"}[5m])) by (job)
  > 0.05`,
							For: lokiv1.PrometheusDuration("10m"),
							Labels: map[string]string{
								"severity":    "low",
								"environment": "production",
							},
							Annotations: map[string]string{
								"summary":  "Low Percentage Latency",
								"playbook": "http://link/to/playbook",
							},
						},
					},
				},
			},
		},
	}

	cfg, err := rules.MarshalAlertingRule(a)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, cfg)
}

func TestMarshalRecordingRule(t *testing.T) {
	expCfg := `
groups:
  - name: a-recording
    interval: 2d
    limit: 1
    rules:
      - expr: |-
          sum(
            rate({container="nginx"}[1m])
          )
        record: nginx:requests:rate1m
      - expr: |-
          sum(
            rate({container="banana"}[5m])
          )
        record: banana:requests:rate5m
`

	r := lokiv1.RecordingRule{
		Spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "a-recording",
					Interval: lokiv1.PrometheusDuration("2d"),
					Limit:    1,
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Expr: `sum(
  rate({container="nginx"}[1m])
)`,
							Record: "nginx:requests:rate1m",
						},
						{
							Expr: `sum(
  rate({container="banana"}[5m])
)`,
							Record: "banana:requests:rate5m",
						},
					},
				},
			},
		},
	}

	cfg, err := rules.MarshalRecordingRule(r)
	fmt.Print(cfg)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, cfg)
}
