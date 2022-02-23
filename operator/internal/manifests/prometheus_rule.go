package manifests

import (
	"bytes"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/grafana/loki/operator/internal/manifests/internal/alerts"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildPrometheusRule returns a list of k8s objects for Loki PrometheusRule
func BuildPrometheusRule(opts Options) ([]client.Object, error) {
	prometheusRule, err := NewPrometheusRule(opts)
	if err != nil {
		return nil, err
	}

	return []client.Object{
		prometheusRule,
	}, nil
}

// NewPrometheusRule creates a prometheus rule
func NewPrometheusRule(opts Options) (*monitoringv1.PrometheusRule, error) {
	alertsBytes, err := alerts.Build()
	if err != nil {
		return nil, err
	}

	var alertsSpec *monitoringv1.PrometheusRuleSpec
	alertsSpec, err = ruleSpec(alertsBytes)
	if err != nil {
		return nil, kverrors.Wrap(err, "Failed to decode alerts yaml")
	}

	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: PrometheusRuleName(opts.Name),
		},
		Spec: *alertsSpec,
	}, nil
}

func ruleSpec(ruleBytes []byte) (*monitoringv1.PrometheusRuleSpec, error) {
	ruleSpec := monitoringv1.PrometheusRuleSpec{}

	ruleReader := bytes.NewReader(ruleBytes)
	err := k8sYAML.NewYAMLOrJSONDecoder(ruleReader, 1000).Decode(&ruleSpec)
	if err != nil {
		return nil, err
	}
	return &ruleSpec, nil
}
