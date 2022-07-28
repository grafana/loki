package manifests

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test that all serviceMonitor match the labels of their services so that we know all serviceMonitor
// will work when deployed.
func TestServiceMonitorMatchLabels(t *testing.T) {
	type test struct {
		Service        *corev1.Service
		ServiceMonitor *monitoringv1.ServiceMonitor
	}

	featureGates := configv1.FeatureGates{
		ServiceMonitors:            true,
		ServiceMonitorTLSEndpoints: true,
		OpenShift: configv1.OpenShiftFeatureGates{
			ServingCertsService: true,
		},
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Gates:     featureGates,
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}

	table := []test{
		{
			Service:        NewDistributorHTTPService(opt),
			ServiceMonitor: NewDistributorServiceMonitor(opt),
		},
		{
			Service:        NewIngesterHTTPService(opt),
			ServiceMonitor: NewIngesterServiceMonitor(opt),
		},
		{
			Service:        NewQuerierHTTPService(opt),
			ServiceMonitor: NewQuerierServiceMonitor(opt),
		},
		{
			Service:        NewQueryFrontendHTTPService(opt),
			ServiceMonitor: NewQueryFrontendServiceMonitor(opt),
		},
		{
			Service:        NewCompactorHTTPService(opt),
			ServiceMonitor: NewCompactorServiceMonitor(opt),
		},
		{
			Service:        NewGatewayHTTPService(opt),
			ServiceMonitor: NewGatewayServiceMonitor(opt),
		},
		{
			Service:        NewIndexGatewayHTTPService(opt),
			ServiceMonitor: NewIndexGatewayServiceMonitor(opt),
		},
		{
			Service:        NewRulerHTTPService(opt),
			ServiceMonitor: NewRulerServiceMonitor(opt),
		},
	}

	for _, tst := range table {
		testName := fmt.Sprintf("%s_%s", tst.Service.GetName(), tst.ServiceMonitor.GetName())
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			for k, v := range tst.ServiceMonitor.Spec.Selector.MatchLabels {
				if assert.Contains(t, tst.Service.Spec.Selector, k) {
					// only assert Equal if the previous assertion is successful or this will panic
					assert.Equal(t, v, tst.Service.Spec.Selector[k])
				}
			}
		})
	}
}

func TestServiceMonitorEndpoints_ForOpenShiftLoggingMode(t *testing.T) {
	featureGates := configv1.FeatureGates{
		LokiStackGateway:           true,
		ServiceMonitors:            true,
		ServiceMonitorTLSEndpoints: true,
		OpenShift: configv1.OpenShiftFeatureGates{
			ServingCertsService: true,
		},
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Gates:     featureGates,
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}

	sm := NewGatewayServiceMonitor(opt)
	require.Len(t, sm.Spec.Endpoints, 2)
}
