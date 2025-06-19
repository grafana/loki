package openshift

import (
	"strings"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func RecordingRuleTenantLabels(r *lokiv1.RecordingRule) {
	switch r.Spec.TenantID {
	case tenantApplication:
		labels := map[string]string{
			ocpMonitoringGroupByLabel: r.Namespace,
		}
		labelMatchers := strings.Split(opaDefaultLabelMatchers, ",")
		for _, label := range labelMatchers {
			labels[label] = r.Namespace
		}
		appendRecordingRuleLabels(r, labels)
	case tenantInfrastructure, tenantAudit, tenantNetwork:
		appendRecordingRuleLabels(r, map[string]string{
			ocpMonitoringGroupByLabel: r.Namespace,
		})
	default:
		// Do nothing
	}
}

func appendRecordingRuleLabels(r *lokiv1.RecordingRule, labels map[string]string) {
	for groupIdx, group := range r.Spec.Groups {
		for ruleIdx, rule := range group.Rules {
			if rule.Labels == nil {
				rule.Labels = map[string]string{}
			}

			for name, value := range labels {
				rule.Labels[name] = value
			}

			group.Rules[ruleIdx] = rule
		}
		r.Spec.Groups[groupIdx] = group
	}
}
