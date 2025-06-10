package openshift

import (
	"strings"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func AlertingRuleTenantLabels(ar *lokiv1.AlertingRule) {
	switch ar.Spec.TenantID {
	case tenantApplication:
		labels := map[string]string{
			ocpMonitoringGroupByLabel: ar.Namespace,
		}
		labelMatchers := strings.Split(opaDefaultLabelMatchers, ",")
		for _, label := range labelMatchers {
			labels[label] = ar.Namespace
		}
		appendAlertingRuleLabels(ar, labels)
	case tenantInfrastructure, tenantAudit, tenantNetwork:
		appendAlertingRuleLabels(ar, map[string]string{
			ocpMonitoringGroupByLabel: ar.Namespace,
		})
	default:
		// Do nothing
	}
}

func appendAlertingRuleLabels(ar *lokiv1.AlertingRule, labels map[string]string) {
	for groupIdx, group := range ar.Spec.Groups {
		for ruleIdx, rule := range group.Rules {
			if rule.Labels == nil {
				rule.Labels = map[string]string{}
			}

			for name, value := range labels {
				rule.Labels[name] = value
			}

			group.Rules[ruleIdx] = rule
		}
		ar.Spec.Groups[groupIdx] = group
	}
}
