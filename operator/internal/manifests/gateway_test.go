package manifests

import (
	"math/rand"
	"reflect"
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestNewGatewayDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	sha1C := "deadbeef"
	ss := NewGatewayDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Compactor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
	}, sha1C)

	expected := "loki.grafana.com/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], sha1C)
}

func TestGatewayConfigMap_ReturnsSHA1OfBinaryContents(t *testing.T) {
	opts := Options{
		Name:      uuid.New().String(),
		Namespace: uuid.New().String(),
		Image:     uuid.New().String(),
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Compactor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.Dynamic,
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: "test",
							},
							IssuerURL:     "https://127.0.0.1:5556/dex",
							RedirectURL:   "https://localhost:8443/oidc/test/callback",
							GroupClaim:    "test",
							UsernameClaim: "test",
						},
					},
				},
				Authorization: &lokiv1beta1.AuthorizationSpec{
					OPA: &lokiv1beta1.OPASpec{
						URL: "http://127.0.0.1:8181/v1/data/observatorium/allow",
					},
				},
			},
		},
		Tenants: Tenants{
			Secrets: []*TenantSecrets{
				{
					TenantName:   "test",
					ClientID:     "test",
					ClientSecret: "test",
					IssuerCAPath: "/tmp/test",
				},
			},
		},
	}

	_, sha1C, err := gatewayConfigMap(opts)
	require.NoError(t, err)
	require.NotEmpty(t, sha1C)
}

func TestBuildGateway_HasConfigForTenantMode(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Flags: FeatureFlags{
			EnableGateway: true,
		},
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)

	d, ok := objs[1].(*appsv1.Deployment)
	require.True(t, ok)
	require.Len(t, d.Spec.Template.Spec.Containers, 2)
}

func TestBuildGateway_HasExtraObjectsForTenantMode(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Flags: FeatureFlags{
			EnableGateway: true,
		},
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				GatewayName:        "abc",
				LokiStackName:      "abc",
				LokiStackNamespace: "efgh",
			},
		},
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, objs, 7)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_RouteSvcMatches(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Flags: FeatureFlags{
			EnableGateway: true,
		},
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				GatewayName:          "abc",
				GatewaySvcName:       serviceNameGatewayHTTP("abcd"),
				GatewaySvcTargetPort: gatewayHTTPPortName,
				LokiStackName:        "abc",
				LokiStackNamespace:   "efgh",
			},
		},
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)

	svc := objs[2].(*corev1.Service)
	rt := objs[3].(*routev1.Route)
	require.Equal(t, svc.Kind, rt.Spec.To.Kind)
	require.Equal(t, svc.Name, rt.Spec.To.Name)
	require.Equal(t, svc.Spec.Ports[0].Name, rt.Spec.Port.TargetPort.StrVal)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_ServiceAccountNameMatches(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Flags: FeatureFlags{
			EnableGateway: true,
		},
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				GatewayName:          GatewayName("abcd"),
				GatewaySvcName:       serviceNameGatewayHTTP("abcd"),
				GatewaySvcTargetPort: gatewayHTTPPortName,
				LokiStackName:        "abc",
				LokiStackNamespace:   "efgh",
			},
		},
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)

	dpl := objs[1].(*appsv1.Deployment)
	sa := objs[4].(*corev1.ServiceAccount)
	require.Equal(t, dpl.Spec.Template.Spec.ServiceAccountName, sa.Name)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_ReplacesIngressWithRoute(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Flags: FeatureFlags{
			EnableGateway: true,
		},
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				GatewayName:          GatewayName("abcd"),
				GatewaySvcName:       serviceNameGatewayHTTP("abcd"),
				GatewaySvcTargetPort: gatewayHTTPPortName,
				LokiStackName:        "abc",
				LokiStackNamespace:   "efgh",
			},
		},
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Gateway: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)

	var kinds []string
	for _, o := range objs {
		kinds = append(kinds, reflect.TypeOf(o).String())
	}

	require.NotContains(t, kinds, "*v1.Ingress")
	require.Contains(t, kinds, "*v1.Route")
}
