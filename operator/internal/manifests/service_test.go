package manifests

import (
	"fmt"
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
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
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}
	sha1C := "deadbef"

	table := []test{
		{
			Containers: NewDistributorDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt),
				NewDistributorHTTPService(opt),
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
			Containers: NewQuerierDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt),
				NewQuerierHTTPService(opt),
			},
		},
		{
			Containers: NewQueryFrontendDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt),
				NewQueryFrontendHTTPService(opt),
			},
		},
		{
			Containers: NewCompactorStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
		{
			Containers: NewGatewayDeployment(opt, sha1C).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewGatewayHTTPService(opt),
			},
		},
		{
			Containers: NewIndexGatewayStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewIndexGatewayGRPCService(opt),
				NewIndexGatewayHTTPService(opt),
			},
		},
		{
			Containers: NewRulerStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewRulerGRPCService(opt),
				NewRulerHTTPService(opt),
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
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}
	sha1C := "deadbef"

	table := []test{
		{
			Object: NewDistributorDeployment(opt),
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt),
				NewDistributorHTTPService(opt),
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
			Object: NewQuerierDeployment(opt),
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt),
				NewQuerierHTTPService(opt),
			},
		},
		{
			Object: NewQueryFrontendDeployment(opt),
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt),
				NewQueryFrontendHTTPService(opt),
			},
		},
		{
			Object: NewCompactorStatefulSet(opt),
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
		{
			Object: NewGatewayDeployment(opt, sha1C),
			Services: []*corev1.Service{
				NewGatewayHTTPService(opt),
			},
		},
		{
			Object: NewIndexGatewayStatefulSet(opt),
			Services: []*corev1.Service{
				NewIndexGatewayGRPCService(opt),
				NewIndexGatewayHTTPService(opt),
			},
		},
		{
			Object: NewRulerStatefulSet(opt),
			Services: []*corev1.Service{
				NewRulerGRPCService(opt),
				NewRulerHTTPService(opt),
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
