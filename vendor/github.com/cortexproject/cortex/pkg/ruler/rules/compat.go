package rules

import (
	time "time"

	"github.com/cortexproject/cortex/pkg/ingester/client"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	legacy_rulefmt "github.com/cortexproject/cortex/pkg/ruler/legacy_rulefmt"
)

// ToProto transforms a formatted prometheus rulegroup to a rule group protobuf
func ToProto(user string, namespace string, rl legacy_rulefmt.RuleGroup) *RuleGroupDesc {
	rg := RuleGroupDesc{
		Name:      rl.Name,
		Namespace: namespace,
		Interval:  time.Duration(rl.Interval),
		Rules:     formattedRuleToProto(rl.Rules),
		User:      user,
	}
	return &rg
}

func formattedRuleToProto(rls []legacy_rulefmt.Rule) []*RuleDesc {
	rules := make([]*RuleDesc, len(rls))
	for i := range rls {
		rules[i] = &RuleDesc{
			Expr:        rls[i].Expr,
			Record:      rls[i].Record,
			Alert:       rls[i].Alert,
			For:         time.Duration(rls[i].For),
			Labels:      client.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Labels)),
			Annotations: client.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Annotations)),
		}
	}

	return rules
}

// FromProto generates a rulefmt RuleGroup
func FromProto(rg *RuleGroupDesc) legacy_rulefmt.RuleGroup {
	formattedRuleGroup := legacy_rulefmt.RuleGroup{
		Name:     rg.GetName(),
		Interval: model.Duration(rg.Interval),
		Rules:    make([]legacy_rulefmt.Rule, len(rg.GetRules())),
	}

	for i, rl := range rg.GetRules() {
		newRule := legacy_rulefmt.Rule{
			Record:      rl.GetRecord(),
			Alert:       rl.GetAlert(),
			Expr:        rl.GetExpr(),
			Labels:      client.FromLabelAdaptersToLabels(rl.Labels).Map(),
			Annotations: client.FromLabelAdaptersToLabels(rl.Annotations).Map(),
			For:         model.Duration(rl.GetFor()),
		}

		formattedRuleGroup.Rules[i] = newRule
	}

	return formattedRuleGroup
}
