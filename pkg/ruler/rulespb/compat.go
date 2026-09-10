package rulespb

import (
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"

	"github.com/grafana/loki/v3/pkg/logproto" //lint:ignore faillint allowed to import other protobuf
)

// ToProto transforms a formatted prometheus rulegroup to a rule group protobuf
func ToProto(user string, namespace string, rl rulefmt.RuleGroup) *RuleGroupDesc {
	rg := RuleGroupDesc{
		Name:      rl.Name,
		Namespace: namespace,
		Interval:  time.Duration(rl.Interval),
		Rules:     formattedRuleToProto(rl.Rules),
		User:      user,
		Limit:     int64(rl.Limit),
	}
	return &rg
}

func formattedRuleToProto(rls []rulefmt.Rule) []*RuleDesc {
	rules := make([]*RuleDesc, len(rls))
	for i := range rls {
		rules[i] = &RuleDesc{
			Expr:        normalizeExpr(rls[i].Expr),
			Record:      rls[i].Record,
			Alert:       rls[i].Alert,
			For:         time.Duration(rls[i].For),
			Labels:      logproto.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Labels)),
			Annotations: logproto.FromLabelsToLabelAdapters(labels.FromMap(rls[i].Annotations)),
		}
	}

	return rules
}

// FromProto generates a rulefmt RuleGroup
func FromProto(rg *RuleGroupDesc) rulefmt.RuleGroup {
	formattedRuleGroup := rulefmt.RuleGroup{
		Name:     rg.GetName(),
		Interval: model.Duration(rg.Interval),
		Rules:    make([]rulefmt.Rule, len(rg.GetRules())),
		Limit:    int(rg.GetLimit()),
	}

	for i, rl := range rg.GetRules() {
		newRule := rulefmt.Rule{
			Expr:        normalizeExpr(rl.GetExpr()),
			Labels:      logproto.FromLabelAdaptersToLabels(rl.Labels).Map(),
			Annotations: logproto.FromLabelAdaptersToLabels(rl.Annotations).Map(),
			For:         model.Duration(rl.GetFor()),
		}

		if rl.GetRecord() != "" {
			newRule.Record = rl.GetRecord()
		} else {
			newRule.Alert = rl.GetAlert()
		}

		formattedRuleGroup.Rules[i] = newRule
	}

	return formattedRuleGroup
}

// normalizeExpr strips whitespace surrounding a rule expression, which is
// insignificant to LogQL but not to the YAML emitter: when a value's first line
// begins with whitespace, yaml.v3 writes a block scalar whose indentation
// indicator disagrees with the indentation it then emits. Nothing can parse the
// result, so the ruler API returns 500 for the whole namespace and the tenant's
// rule manager refuses to load any of its files. Applied on the way in so stored
// groups round trip, and on the way out so groups stored before this fix are
// still readable.
func normalizeExpr(expr string) string {
	return strings.TrimSpace(expr)
}
