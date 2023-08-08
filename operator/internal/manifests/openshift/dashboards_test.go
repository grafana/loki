package openshift

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildDashboards_ReturnsDashboardConfigMaps(t *testing.T) {
	objs, err := BuildDashboards("test")
	require.NoError(t, err)

	for _, d := range objs {
		switch d.(type) {
		case *corev1.ConfigMap:
			require.Equal(t, d.GetNamespace(), managedConfigNamespace)
			require.Contains(t, d.GetLabels(), labelConsoleDashboard)
		}
	}
}

func TestBuildDashboards_ReturnsPrometheusRules(t *testing.T) {
	objs, err := BuildDashboards("test")
	require.NoError(t, err)

	rules := objs[len(objs)-1].(*monitoringv1.PrometheusRule)
	require.Equal(t, rules.GetName(), dashboardPrometheusRulesName)
	require.Equal(t, rules.GetNamespace(), "test")
	require.NotNil(t, rules.Spec)
}
