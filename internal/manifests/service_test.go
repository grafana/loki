package manifests

import (
	"fmt"
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test that the service ports have matching deployment/statefulset/daemonset ports on the podspec.
func TestServicesMatchPorts(t *testing.T) {
	type test struct {
		Services   []*corev1.Service
		Containers []corev1.Container
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
			Containers: NewDistributorDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt.Name),
				NewDistributorHTTPService(opt.Name),
			},
		},
		{
			Containers: NewIngesterStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewIngesterGRPCService(opt),
				NewIngesterHTTPService(opt),
			},
		},
		{
			Containers: NewQuerierStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt.Name),
				NewQuerierHTTPService(opt.Name),
			},
		},
		{
			Containers: NewQueryFrontendDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt.Name),
				NewQueryFrontendHTTPService(opt.Name),
			},
		},
		{
			Containers: NewCompactorStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
	}

	containerHasPort := func(containers []corev1.Container, port int32) bool {
		for _, container := range containers {
			for _, p := range container.Ports {
				if p.ContainerPort == port {
					return true
				}
			}
		}
		return false
	}

	for _, tst := range table {
		for _, service := range tst.Services {
			for _, port := range service.Spec.Ports {
				// rescope for t.Parallel
				tst, service, port := tst, service, port
				testName := fmt.Sprintf("%s_%d", service.GetName(), port.Port)
				t.Run(testName, func(t *testing.T) {
					t.Parallel()
					found := containerHasPort(tst.Containers, port.Port)
					assert.True(t, found, "Service port (%d) does not match any port in the defined containers", port.Port)
				})
			}
		}
	}
}

// Test that all services match the labels of their deployments/statefulsets so that we know all services will
// work when deployed.
func TestServicesMatchLabels(t *testing.T) {
	type test struct {
		Services []*corev1.Service
		Object   client.Object
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
			Object: NewDistributorDeployment(opt),
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt.Name),
				NewDistributorHTTPService(opt.Name),
			},
		},
		{
			Object: NewIngesterStatefulSet(opt),
			Services: []*corev1.Service{
				NewIngesterGRPCService(opt),
				NewIngesterHTTPService(opt),
			},
		},
		{
			Object: NewQuerierStatefulSet(opt),
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt.Name),
				NewQuerierHTTPService(opt.Name),
			},
		},
		{
			Object: NewQueryFrontendDeployment(opt),
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt.Name),
				NewQueryFrontendHTTPService(opt.Name),
			},
		},
		{
			Object: NewCompactorStatefulSet(opt),
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
	}

	for _, tst := range table {
		for _, service := range tst.Services {
			// rescope for t.Parallel()
			tst, service := tst, service

			testName := fmt.Sprintf("%s_%s", tst.Object.GetName(), service.GetName())
			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				for k, v := range service.Spec.Selector {
					if assert.Contains(t, tst.Object.GetLabels(), k) {
						// only assert Equal if the previous assertion is successful or this will panic
						assert.Equal(t, v, tst.Object.GetLabels()[k])
					}
				}
			})
		}
	}
}
