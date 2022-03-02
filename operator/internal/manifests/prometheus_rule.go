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
	alertsSpec, err := getRuleSpec(newAlertsSpec)
	if err != nil {
		return nil, kverrors.Wrap(err, "Failed to get alerts spec")
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

type newAlertsSpecFunc func() (*monitoringv1.PrometheusRuleSpec, error)

var cachedRuleSpec *monitoringv1.PrometheusRuleSpec

// getRuleSpec returns a PrometheusRuleSpec of alerts. It calls `fn()` and caches its value to avoid subsequent calls.
// Since the alerts are defined in a static yaml file, we can cache the decoded result rather than re-decoding it in
// every reconcile loop.
func getRuleSpec(fn newAlertsSpecFunc) (*monitoringv1.PrometheusRuleSpec, error) {
	if cachedRuleSpec != nil {
		return cachedRuleSpec, nil
	}

	alertsSpec, err := fn()
	if err != nil {
		return nil, err
	}
	cachedRuleSpec = alertsSpec
	return alertsSpec, nil
}

func newAlertsSpec() (*monitoringv1.PrometheusRuleSpec, error) {
	alertsBytes, err := alerts.Build()
	if err != nil {
		return nil, err
	}

	var alertsSpec *monitoringv1.PrometheusRuleSpec
	r := bytes.NewReader(alertsBytes)
	err = k8sYAML.NewYAMLOrJSONDecoder(r, 1000).Decode(&alertsSpec)
	if err != nil {
		return nil, err
	}
	return alertsSpec, nil
}
