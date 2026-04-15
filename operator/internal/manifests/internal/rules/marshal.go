package rules

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	"gopkg.in/yaml.v2"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

const tenantLabel = "tenantId"

type alertingRuleSpec struct {
	Groups []*lokiv1.AlertingRuleGroup `json:"groups"`
}

type recordingRuleSpec struct {
	Groups []*lokiv1.RecordingRuleGroup `json:"groups"`
}

// MarshalAlertingRule returns the alerting rule groups marshaled into YAML or an error.
func MarshalAlertingRule(a lokiv1.AlertingRule) (string, error) {
	aa := a.DeepCopy()
	ar := alertingRuleSpec{
		Groups: aa.Spec.Groups,
	}

	for _, group := range ar.Groups {
		for _, rule := range group.Rules {
			if rule.Labels == nil {
				rule.Labels = map[string]string{}
			}

			rule.Labels[tenantLabel] = aa.Spec.TenantID
		}
	}

	content, err := yaml.Marshal(ar)
	if err != nil {
		return "", kverrors.Wrap(err, "failed to marshal alerting rule", "name", aa.Name, "namespace", aa.Namespace)
	}

	return string(content), nil
}

// MarshalRecordingRule returns the recording rule groups marshaled into YAML or an error.
func MarshalRecordingRule(a lokiv1.RecordingRule) (string, error) {
	aa := a.DeepCopy()
	ar := recordingRuleSpec{
		Groups: aa.Spec.Groups,
	}

	for _, group := range ar.Groups {
		for _, rule := range group.Rules {
			if rule.Labels == nil {
				rule.Labels = map[string]string{}
			}

			rule.Labels[tenantLabel] = aa.Spec.TenantID
		}
	}

	content, err := yaml.Marshal(ar)
	if err != nil {
		return "", kverrors.Wrap(err, "failed to marshal recording rule", "name", aa.Name, "namespace", aa.Namespace)
	}

	return string(content), nil
}
