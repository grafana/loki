package manifests

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/manifests/internal/alerts"
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
	alertOpts := alerts.Options{
		RunbookURL: alerts.RunbookDefaultURL,
	}

	spec, err := alerts.Build(alertOpts)
	if err != nil {
		return nil, err
	}

	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: PrometheusRuleName(opts.Name),
		},
		Spec: *spec,
	}, nil
}
