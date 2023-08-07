package openshift

import (
	"encoding/json"
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

func BuildDashboards(opt Options) ([]client.Object, error) {
	ds, rules := dashboards.ReadFiles()

	var objs []client.Object
	for name, content := range ds {
		objs = append(objs, newDashboardConfigMap(name, content))
	}

	promRule, err := newDashboardPrometheusRule(opt, rules)
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

func newDashboardPrometheusRule(opt Options, content []byte) (*monitoringv1.PrometheusRule, error) {
	spec := &monitoringv1.PrometheusRuleSpec{}

	err := json.Unmarshal(content, spec)
	if err != nil {
		return nil, err
	}

	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PrometheusRule",
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dashboardPrometheusRulesName(opt),
			Namespace: opt.BuildOpts.LokiStackNamespace,
			Labels:    opt.BuildOpts.Labels,
		},
		Spec: *spec,
	}, nil
}
