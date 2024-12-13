package manifests

import (
	"math/rand"
	"path"
	"reflect"
	"testing"

	"github.com/google/uuid"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/gateway"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestNewGatewayDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	sha1C := "deadbeef"
	ss := NewGatewayDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}, sha1C)

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiConfigHash)
	require.Equal(t, annotations[AnnotationLokiConfigHash], sha1C)
}

func TestNewGatewayDeployment_HasNotTemplateObjectStoreHashAnnotation(t *testing.T) {
	sha1C := "deadbeef"
	ss := NewGatewayDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		ObjectStorage: storage.Options{
			SecretSHA1: "deadbeef",
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}, sha1C)

	annotations := ss.Spec.Template.Annotations
	require.NotContains(t, annotations, AnnotationLokiObjectStoreHash)
}

func TestNewGatewayDeployment_HasNodeSelector(t *testing.T) {
	toleration := []corev1.Toleration{
		{
			Key:      "foo",
			Operator: corev1.TolerationOpEqual,
			Value:    "bar",
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	selector := map[string]string{
		"foo": "bar",
	}
	dpl := NewGatewayDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas:     rand.Int31(),
					NodeSelector: selector,
					Tolerations:  toleration,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}, "deadbeef")

	require.Equal(t, dpl.Spec.Template.Spec.NodeSelector, selector)
	require.ElementsMatch(t, dpl.Spec.Template.Spec.Tolerations, toleration)
}

func TestNewGatewayDeployment_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	sha1C := "deadbeef"
	ss := NewGatewayDeployment(Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}, sha1C)

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationCertRotationRequiredAt)
	require.Equal(t, annotations[AnnotationCertRotationRequiredAt], "deadbeef")
}

func TestGatewayConfigMap_ReturnsSHA1OfBinaryContents(t *testing.T) {
	opts := Options{
		Name:      uuid.New().String(),
		Namespace: uuid.New().String(),
		Image:     uuid.New().String(),
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Dynamic,
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: "test",
							},
							IssuerURL:     "https://127.0.0.1:5556/dex",
							RedirectURL:   "https://localhost:8443/oidc/test/callback",
							GroupClaim:    "test",
							UsernameClaim: "test",
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "http://127.0.0.1:8181/v1/data/observatorium/allow",
					},
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
		Tenants: Tenants{
			Secrets: []*TenantSecrets{
				{
					TenantName: "test",
					OIDCSecret: &OIDCSecret{
						ClientID:     "test",
						ClientSecret: "test",
						IssuerCAPath: "/tmp/test",
					},
				},
			},
		},
	}

	_, _, sha1C, err := gatewayConfigObjs(opts)
	require.NoError(t, err)
	require.NotEmpty(t, sha1C)
}

func TestBuildGateway_HasConfigForTenantMode(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)

	d, ok := objs[2].(*appsv1.Deployment)
	require.True(t, ok)
	require.Len(t, d.Spec.Template.Spec.Containers, 2)
}

func TestBuildGateway_HasExtraObjectsForTenantMode(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
			OpenShift: configv1.OpenShiftFeatureGates{
				Enabled: true,
			},
		},
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				GatewayName:        "abc",
				LokiStackName:      "abc",
				LokiStackNamespace: "efgh",
			},
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)
	require.Len(t, objs, 13)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_RouteSvcMatches(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
			OpenShift: configv1.OpenShiftFeatureGates{
				Enabled: true,
			},
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
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)

	svc := objs[5].(*corev1.Service)
	rt := objs[7].(*routev1.Route)
	require.Equal(t, svc.Kind, rt.Spec.To.Kind)
	require.Equal(t, svc.Name, rt.Spec.To.Name)
	require.Equal(t, svc.Spec.Ports[0].Name, rt.Spec.Port.TargetPort.StrVal)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_ServiceAccountNameMatches(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
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
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)

	dpl := objs[2].(*appsv1.Deployment)
	sa := objs[3].(*corev1.ServiceAccount)
	require.Equal(t, dpl.Spec.Template.Spec.ServiceAccountName, sa.Name)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_ReplacesIngressWithRoute(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
			OpenShift: configv1.OpenShiftFeatureGates{
				Enabled: true,
			},
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
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)

	var kinds []string
	for _, o := range objs {
		kinds = append(kinds, reflect.TypeOf(o).String())
	}

	require.NotContains(t, kinds, "*v1.Ingress")
	require.Contains(t, kinds, "*v1.Route")
}

func TestBuildGateway_WithTLSProfile(t *testing.T) {
	tt := []struct {
		desc         string
		options      Options
		expectedArgs []string
	}{
		{
			desc: "static mode",
			options: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					HTTPEncryption:   true,
					TLSProfile:       string(configv1.TLSProfileOldType),
				},
				TLSProfile: TLSProfileSpec{
					MinTLSVersion: "min-version",
					Ciphers:       []string{"cipher1", "cipher2"},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"metrics"},
									Tenants:     []string{"test-a"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "test-a",
									Subjects: []lokiv1.Subject{
										{
											Name: "test@example.com",
											Kind: "user",
										},
									},
									Roles: []string{"read-write"},
								},
							},
						},
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			expectedArgs: []string{
				"--tls.min-version=min-version",
				"--tls.cipher-suites=cipher1,cipher2",
			},
		},
		{
			desc: "dynamic mode",
			options: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					HTTPEncryption:   true,
					TLSProfile:       string(configv1.TLSProfileOldType),
				},
				TLSProfile: TLSProfileSpec{
					MinTLSVersion: "min-version",
					Ciphers:       []string{"cipher1", "cipher2"},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			expectedArgs: []string{
				"--tls.min-version=min-version",
				"--tls.cipher-suites=cipher1,cipher2",
			},
		},
		{
			desc: "openshift-logging mode",
			options: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					HTTPEncryption:   true,
					TLSProfile:       string(configv1.TLSProfileOldType),
				},
				TLSProfile: TLSProfileSpec{
					MinTLSVersion: "min-version",
					Ciphers:       []string{"cipher1", "cipher2"},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			expectedArgs: []string{
				"--tls.min-version=min-version",
				"--tls.cipher-suites=cipher1,cipher2",
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			objs, err := BuildGateway(tc.options)
			require.NoError(t, err)

			d, ok := objs[2].(*appsv1.Deployment)
			require.True(t, ok)

			for _, arg := range tc.expectedArgs {
				require.Contains(t, d.Spec.Template.Spec.Containers[0].Args, arg)
			}
		})
	}
}

func TestBuildGateway_WithRulesEnabled(t *testing.T) {
	tt := []struct {
		desc        string
		opts        Options
		wantArgs    []string
		missingArgs []string
	}{
		{
			desc: "rules disabled",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"metrics"},
									Tenants:     []string{"test-a"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "test-a",
									Subjects: []lokiv1.Subject{
										{
											Name: "test@example.com",
											Kind: "user",
										},
									},
									Roles: []string{"read-write"},
								},
							},
						},
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			missingArgs: []string{
				"--logs.rules.endpoint=http://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
			},
		},
		{
			desc: "static mode",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"metrics"},
									Tenants:     []string{"test-a"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "test-a",
									Subjects: []lokiv1.Subject{
										{
											Name: "test@example.com",
											Kind: "user",
										},
									},
									Roles: []string{"read-write"},
								},
							},
						},
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			wantArgs: []string{
				"--logs.rules.endpoint=http://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
			},
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			wantArgs: []string{
				"--logs.rules.endpoint=http://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
			},
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					HTTPEncryption:   true,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			wantArgs: []string{
				"--logs.rules.endpoint=https://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
				"--logs.rules.label-filters=application:kubernetes_namespace_name",
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					HTTPEncryption:   true,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
			wantArgs: []string{
				"--logs.rules.endpoint=https://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			objs, err := BuildGateway(tc.opts)
			require.NoError(t, err)

			d, ok := objs[2].(*appsv1.Deployment)
			require.True(t, ok)

			for _, arg := range tc.wantArgs {
				require.Contains(t, d.Spec.Template.Spec.Containers[0].Args, arg)
			}
			for _, arg := range tc.missingArgs {
				require.NotContains(t, d.Spec.Template.Spec.Containers[0].Args, arg)
			}
		})
	}
}

func TestBuildGateway_WithHTTPEncryption(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
			HTTPEncryption:   true,
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Rules: &lokiv1.RulesSpec{
				Enabled: true,
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode:           lokiv1.Static,
				Authorization:  &lokiv1.AuthorizationSpec{},
				Authentication: []lokiv1.AuthenticationSpec{},
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	require.NoError(t, err)

	dpl := objs[2].(*appsv1.Deployment)
	require.NotNil(t, dpl)
	require.Len(t, dpl.Spec.Template.Spec.Containers, 1)

	c := dpl.Spec.Template.Spec.Containers[0]

	expectedArgs := []string{
		"--debug.name=lokistack-gateway",
		"--web.listen=0.0.0.0:8080",
		"--web.internal.listen=0.0.0.0:8081",
		"--web.healthchecks.url=https://localhost:8080",
		"--log.level=warn",
		"--logs.read.endpoint=https://abcd-query-frontend-http.efgh.svc.cluster.local:3100",
		"--logs.tail.endpoint=https://abcd-query-frontend-http.efgh.svc.cluster.local:3100",
		"--logs.write.endpoint=https://abcd-distributor-http.efgh.svc.cluster.local:3100",
		"--logs.write-timeout=4m0s",
		"--rbac.config=/etc/lokistack-gateway/rbac.yaml",
		"--tenants.config=/etc/lokistack-gateway/tenants.yaml",
		"--server.read-timeout=48s",
		"--server.write-timeout=6m0s",
		"--logs.rules.endpoint=https://abcd-ruler-http.efgh.svc.cluster.local:3100",
		"--logs.rules.read-only=true",
		"--tls.client-auth-type=NoClientCert",
		"--tls.min-version=VersionTLS12",
		"--tls.server.cert-file=/var/run/tls/http/server/tls.crt",
		"--tls.server.key-file=/var/run/tls/http/server/tls.key",
		"--tls.healthchecks.server-ca-file=/var/run/ca/server/service-ca.crt",
		"--tls.healthchecks.server-name=abcd-gateway-http.efgh.svc.cluster.local",
		"--tls.internal.server.cert-file=/var/run/tls/http/server/tls.crt",
		"--tls.internal.server.key-file=/var/run/tls/http/server/tls.key",
		"--tls.min-version=",
		"--tls.cipher-suites=",
		"--logs.tls.ca-file=/var/run/ca/upstream/service-ca.crt",
		"--logs.tls.cert-file=/var/run/tls/http/upstream/tls.crt",
		"--logs.tls.key-file=/var/run/tls/http/upstream/tls.key",
	}
	require.Equal(t, expectedArgs, c.Args)

	expectedVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "rbac",
			ReadOnly:  true,
			MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRbacFileName),
			SubPath:   "rbac.yaml",
		},
		{
			Name:      "tenants",
			ReadOnly:  true,
			MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayTenantFileName),
			SubPath:   "tenants.yaml",
		},
		{
			Name:      "lokistack-gateway",
			ReadOnly:  true,
			MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRegoFileName),
			SubPath:   "lokistack-gateway.rego",
		},
		{
			Name:      "tls-secret",
			ReadOnly:  true,
			MountPath: "/var/run/tls/http/server",
		},
		{
			Name:      "abcd-gateway-client-http",
			ReadOnly:  true,
			MountPath: "/var/run/tls/http/upstream",
		},
		{
			Name:      "abcd-ca-bundle",
			ReadOnly:  true,
			MountPath: "/var/run/ca/upstream",
		},
		{
			Name:      "abcd-gateway-ca-bundle",
			ReadOnly:  true,
			MountPath: "/var/run/ca/server",
		},
	}
	require.Equal(t, expectedVolumeMounts, c.VolumeMounts)

	expectedVolumes := []corev1.Volume{
		{
			Name: "rbac",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "abcd-gateway",
					},
				},
			},
		},
		{
			Name: "tenants",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "abcd-gateway",
				},
			},
		},
		{
			Name: "lokistack-gateway",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "abcd-gateway",
					},
				},
			},
		},
		{
			Name: "tls-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "abcd-gateway-http",
				},
			},
		},
		{
			Name: "abcd-gateway-client-http",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "abcd-gateway-client-http",
				},
			},
		},
		{
			Name: "abcd-ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &defaultConfigMapMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "abcd-ca-bundle",
					},
				},
			},
		},
		{
			Name: "abcd-gateway-ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &defaultConfigMapMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "abcd-gateway-ca-bundle",
					},
				},
			},
		},
	}
	require.Equal(t, expectedVolumes, dpl.Spec.Template.Spec.Volumes)
}

func TestBuildGateway_PodDisruptionBudget(t *testing.T) {
	opts := Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
			OpenShift: configv1.OpenShiftFeatureGates{
				Enabled: true,
			},
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	}
	objs, err := BuildGateway(opts)
	require.NoError(t, err)
	require.Len(t, objs, 13)

	pdb := objs[6].(*policyv1.PodDisruptionBudget)
	require.NotNil(t, pdb)
	require.Equal(t, "abcd-gateway", pdb.Name)
	require.Equal(t, "efgh", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
	require.Equal(t, int32(1), pdb.Spec.MinAvailable.IntVal)
	require.EqualValues(t, ComponentLabels(LabelGatewayComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
}

func TestBuildGateway_TopologySpreadConstraint(t *testing.T) {
	obj, _ := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
		},
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "zone",
						MaxSkew:     2,
					},
					{
						TopologyKey: "region",
						MaxSkew:     1,
					},
				},
				Factor: 1,
			},
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		Timeouts: defaultTimeoutConfig,
	})

	dpl := obj[2].(*appsv1.Deployment)
	require.EqualValues(t, dpl.Spec.Template.Spec.TopologySpreadConstraints, []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           2,
			TopologyKey:       "zone",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "lokistack-gateway",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
		{
			MaxSkew:           1,
			TopologyKey:       "region",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "lokistack-gateway",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
	})
}
