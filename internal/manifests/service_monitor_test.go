package manifests

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
)

// Test that all serviceMonitor match the labels of their services so that we know all serviceMonitor
// will work when deployed.
func TestServiceMonitorMatchLabels(t *testing.T) {
	type test struct {
		Service        *corev1.Service
		ServiceMonitor *monitoringv1.ServiceMonitor
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Template: &lokiv1beta1.LokiTemplateSpec{
				Compactor: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}

	table := []test{
		{
			Service:        NewDistributorHTTPService(opt.Name),
			ServiceMonitor: NewDistributorServiceMonitor(opt.Name, opt.Namespace),
		},
		{
			Service:        NewIngesterHTTPService(opt),
			ServiceMonitor: NewIngesterServiceMonitor(opt.Name, opt.Namespace),
		},
		{
			Service:        NewQuerierHTTPService(opt.Name),
			ServiceMonitor: NewQuerierServiceMonitor(opt.Name, opt.Namespace),
		},
		{
			Service:        NewQueryFrontendHTTPService(opt.Name),
			ServiceMonitor: NewQueryFrontendServiceMonitor(opt.Name, opt.Namespace),
		},
		{
			Service:        NewCompactorHTTPService(opt),
			ServiceMonitor: NewCompactorServiceMonitor(opt.Name, opt.Namespace),
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
