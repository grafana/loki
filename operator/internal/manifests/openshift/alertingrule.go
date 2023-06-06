package openshift

import lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

func AlertingRuleTenantLabels(ar *lokiv1.AlertingRule) {
	switch ar.Spec.TenantID {
	case tenantApplication:
		for groupIdx, group := range ar.Spec.Groups {
			group := group
			for ruleIdx, rule := range group.Rules {
				rule := rule
				if rule.Labels == nil {
					rule.Labels = map[string]string{}
				}
				rule.Labels[opaDefaultLabelMatcher] = ar.Namespace
				group.Rules[ruleIdx] = rule
			}
			ar.Spec.Groups[groupIdx] = group
		}
	case tenantInfrastructure, tenantAudit:
		// Do nothing
	case tenantNetwork:
		// Do nothing
	default:
		// Do nothing
	}
}
