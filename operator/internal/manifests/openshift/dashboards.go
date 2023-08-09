package openshift

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/grafana/loki/operator/internal/manifests/openshift/internal/dashboards"
)

const (
	labelConsoleDashboard  = "console.openshift.io/dashboard"
	managedConfigNamespace = "openshift-config-managed"
)

func BuildDashboards(operatorNs string) ([]client.Object, error) {
	ds, rules := dashboards.Content()

	var objs []client.Object
	for name, content := range ds {
		objs = append(objs, newDashboardConfigMap(name, content))
	}

	promRule, err := newDashboardPrometheusRule(operatorNs, rules)
	if err != nil {
		return nil, err
	}
	objs = append(objs, promRule)

	return objs, nil
}

func newDashboardConfigMap(filename string, content []byte) *corev1.ConfigMap {
	cmName := strings.Split(filename, ".")[0]

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: managedConfigNamespace,
			Labels: map[string]string{
				labelConsoleDashboard: "true",
			},
		},
		Data: map[string]string{
			filename: string(content),
		},
	}
}

func newDashboardPrometheusRule(namespace string, spec *monitoringv1.PrometheusRuleSpec) (*monitoringv1.PrometheusRule, error) {
	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PrometheusRule",
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dashboardPrometheusRulesName,
			Namespace: namespace,
		},
		Spec: *spec,
	}, nil
}
