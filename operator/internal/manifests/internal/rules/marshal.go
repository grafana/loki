package rules

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"gopkg.in/yaml.v2"
)

type alertingRuleSpec struct {
	Groups []*lokiv1beta1.AlertingRuleGroup `json:"groups"`
}

type recordingRuleSpec struct {
	Groups []*lokiv1beta1.RecordingRuleGroup `json:"groups"`
}

// MarshalAlertingRule returns the alerting rule groups marshaled into YAML or an error.
func MarshalAlertingRule(a lokiv1beta1.AlertingRule) (string, error) {
	ar := alertingRuleSpec{
		Groups: a.Spec.Groups,
	}

	content, err := yaml.Marshal(ar)
	if err != nil {
		return "", kverrors.Wrap(err, "failed to marshal alerting rule", "name", a.Name, "namespace", a.Namespace)
	}

	return string(content), nil
}

// MarshalRecordingRule returns the recording rule groups marshaled into YAML or an error.
func MarshalRecordingRule(a lokiv1beta1.RecordingRule) (string, error) {
	ar := recordingRuleSpec{
		Groups: a.Spec.Groups,
	}

	content, err := yaml.Marshal(ar)
	if err != nil {
		return "", kverrors.Wrap(err, "failed to marshal recording rule", "name", a.Name, "namespace", a.Namespace)
	}

	return string(content), nil
}
