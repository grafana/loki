package rules

import (
	time "time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	"gopkg.in/yaml.v3"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

// ToProto transforms a formatted prometheus rulegroup to a rule group protobuf
func ToProto(user string, namespace string, rl rulefmt.RuleGroup) *RuleGroupDesc {
	rg := RuleGroupDesc{
		Name:      rl.Name,
		Namespace: namespace,
		Interval:  time.Duration(rl.Interval),
		Rules:     formattedRuleToProto(rl.Rules),
		User:      user,
	}
	return &rg
}

func formattedRuleToProto(rls []rulefmt.RuleNode) []*RuleDesc {
	rules := make([]*RuleDesc, len(rls))
	for i := range rls {
		rules[i] = &RuleDesc{
			Expr:        rls[i].Expr.Value,
			Record:      rls[i].Record.Value,
			Alert:       rls[i].Alert.Value,
			For:         time.Duration(rls[i].For),
			Labels:      client.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Labels)),
			Annotations: client.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Annotations)),
		}
	}

	return rules
}

// FromProto generates a rulefmt RuleGroup
func FromProto(rg *RuleGroupDesc) rulefmt.RuleGroup {
	formattedRuleGroup := rulefmt.RuleGroup{
		Name:     rg.GetName(),
		Interval: model.Duration(rg.Interval),
		Rules:    make([]rulefmt.RuleNode, len(rg.GetRules())),
	}

	for i, rl := range rg.GetRules() {
		recordNode := yaml.Node{}
		recordNode.SetString(rl.GetRecord())
		alertNode := yaml.Node{}
		alertNode.SetString(rl.GetAlert())
		exprNode := yaml.Node{}
		exprNode.SetString(rl.GetExpr())
		newRule := rulefmt.RuleNode{
			Record:      recordNode,
			Alert:       alertNode,
			Expr:        exprNode,
			Labels:      client.FromLabelAdaptersToLabels(rl.Labels).Map(),
			Annotations: client.FromLabelAdaptersToLabels(rl.Annotations).Map(),
			For:         model.Duration(rl.GetFor()),
		}

		formattedRuleGroup.Rules[i] = newRule
	}

	return formattedRuleGroup
}
