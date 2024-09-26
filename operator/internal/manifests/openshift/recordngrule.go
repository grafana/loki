package openshift

import lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

func RecordingRuleTenantLabels(r *lokiv1.RecordingRule) {
	switch r.Spec.TenantID {
	case tenantApplication:
		appendRecordingRuleLabels(r, map[string]string{
			opaDefaultLabelMatcher:    r.Namespace,
			ocpMonitoringGroupByLabel: r.Namespace,
		})
	case tenantInfrastructure, tenantAudit, tenantNetwork:
		appendRecordingRuleLabels(r, map[string]string{
			ocpMonitoringGroupByLabel: r.Namespace,
		})
	default:
		// Do nothing
	}
}

func appendRecordingRuleLabels(ar *lokiv1.RecordingRule, labels map[string]string) {
	for groupIdx, group := range ar.Spec.Groups {
		group := group
		for ruleIdx, rule := range group.Rules {
			rule := rule
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
