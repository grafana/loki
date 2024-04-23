package userconfig

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var legacyRulesFile = `ALERT TestAlert
IF up == 0
FOR 5m
LABELS { severity = "critical" }
ANNOTATIONS {
	message = "I am a message"
}`

var ruleFile = `groups:
- name: example
  rules:
  - alert: TestAlert
    expr: up == 0
    for: 5m
    labels:
      severity: critical
    annotations:
      message: I am a message`

func TestUnmarshalJSONLegacyConfigWithMissingRuleFormatVersionSucceeds(t *testing.T) {
	actual := Config{}
	buf := []byte(`{"rules_files": {"a": "b"}}`)
	assert.Nil(t, json.Unmarshal(buf, &actual))

	expected := Config{
		RulesConfig: RulesConfig{
			Files: map[string]string{
				"a": "b",
			},
			FormatVersion: RuleFormatV1,
		},
	}

	assert.Equal(t, expected, actual)
}

func TestUnmarshalYAMLLegacyConfigWithMissingRuleFormatVersionSucceeds(t *testing.T) {
	actual := Config{}
	buf := []byte(strings.TrimSpace(`
rule_format_version: '1'
rules_files:
  a: b
`))
	assert.Nil(t, yaml.Unmarshal(buf, &actual))

	expected := Config{
		RulesConfig: RulesConfig{
			Files: map[string]string{
				"a": "b",
			},
			FormatVersion: RuleFormatV1,
		},
	}

	assert.Equal(t, expected, actual)
}

func TestParseLegacyAlerts(t *testing.T) {
	parsed, err := parser.ParseExpr("up == 0")
	require.NoError(t, err)
	rule := rules.NewAlertingRule(
		"TestAlert",
		parsed,
		5*time.Minute,
		0,
		labels.Labels{
			labels.Label{Name: "severity", Value: "critical"},
		},
		labels.Labels{
			labels.Label{Name: "message", Value: "I am a message"},
		},
		nil,
		"",
		true,
		log.With(util_log.Logger, "alert", "TestAlert"),
	)

	for i, tc := range []struct {
		cfg      RulesConfig
		expected map[string][]rules.Rule
		wantErr  error
	}{
		{
			cfg: RulesConfig{
				FormatVersion: RuleFormatV1,
				Files: map[string]string{
					"legacy.rules": `
		ALERT TestAlert
		IF up == 0
		FOR 5m
		LABELS { severity = "critical" }
		ANNOTATIONS {
			message = "I am a message"
		}
		`,
				},
			},
			expected: map[string][]rules.Rule{
				"legacy.rules": {rule},
			},
			wantErr: fmt.Errorf("version 0 isn't supported"),
		},
		{
			cfg: RulesConfig{
				FormatVersion: RuleFormatV2,
				Files: map[string]string{
					"alerts.yaml": `
groups:
- name: example
  rules:
  - alert: TestAlert
    expr: up == 0
    for: 5m
    labels:
      severity: critical
    annotations:
      message: I am a message
`,
				},
			},
			expected: map[string][]rules.Rule{
				"example;alerts.yaml": {rule},
			},
			wantErr: nil,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			rules, err := tc.cfg.Parse()
			if tc.wantErr != nil {
				require.EqualError(t, err, tc.wantErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, rules)
			}
		})
	}
}

func TestParseFormatted(t *testing.T) {
	dur, err := model.ParseDuration("5m")
	require.NoError(t, err)

	rulesV1 := []rulefmt.RuleNode{
		{
			Alert: yaml.Node{Value: "TestAlert"},
			Expr:  yaml.Node{Value: "up == 0"},
			For:   dur,
			Labels: map[string]string{
				"severity": "critical",
			},
			Annotations: map[string]string{
				"message": "I am a message",
			},
		},
	}

	alertNode := yaml.Node{Line: 4, Column: 12}
	alertNode.SetString("TestAlert")
	exprNode := yaml.Node{Line: 5, Column: 11}
	exprNode.SetString("up == 0")
	rulesV2 := []rulefmt.RuleNode{
		{
			Alert: alertNode,
			Expr:  exprNode,
			For:   dur,
			Labels: map[string]string{
				"severity": "critical",
			},
			Annotations: map[string]string{
				"message": "I am a message",
			},
		},
	}

	for i, tc := range []struct {
		cfg      RulesConfig
		expected map[string]rulefmt.RuleGroups
		wantErr  error
	}{
		{
			cfg: RulesConfig{
				FormatVersion: RuleFormatV1,
				Files: map[string]string{
					"legacy.rules": legacyRulesFile,
				},
			},
			expected: map[string]rulefmt.RuleGroups{
				"legacy.rules": {
					Groups: []rulefmt.RuleGroup{
						{
							Name:  "rg:legacy.rules",
							Rules: rulesV1,
						},
					},
				},
			},
			wantErr: fmt.Errorf("version 0 isn't supported"),
		},
		{
			cfg: RulesConfig{
				FormatVersion: RuleFormatV2,
				Files: map[string]string{
					"alerts.yaml": ruleFile,
				},
			},
			expected: map[string]rulefmt.RuleGroups{
				"alerts.yaml": {
					Groups: []rulefmt.RuleGroup{
						{
							Name:  "example",
							Rules: rulesV2,
						},
					},
				},
			},
			wantErr: nil,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			rules, err := tc.cfg.ParseFormatted()
			if tc.wantErr != nil {
				require.EqualError(t, err, tc.wantErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, rules)
			}
		})
	}
}
