package commands

import (
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

func TestCheckDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []rwrulefmt.RuleGroup
		want []compareRuleType
	}{
		{
			name: "no duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.RuleNode{
						{
							Record: yaml.Node{Value: "up"},
							Expr:   yaml.Node{Value: "up==1"},
						},
						{
							Record: yaml.Node{Value: "down"},
							Expr:   yaml.Node{Value: "up==0"},
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: nil,
		},
		{
			name: "with duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.RuleNode{
						{
							Record: yaml.Node{Value: "up"},
							Expr:   yaml.Node{Value: "up==1"},
						},
						{
							Record: yaml.Node{Value: "up"},
							Expr:   yaml.Node{Value: "up==0"},
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: []compareRuleType{{metric: "up", label: map[string]string(nil)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, checkDuplicates(tc.in))
		})
	}
}
