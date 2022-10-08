package manifests

import (
	"math/rand"
	"reflect"
	"testing"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
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
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1.LokiComponentSpec{
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
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
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
	})

	require.NoError(t, err)
	require.Len(t, objs, 9)
}

func TestBuildGateway_WithExtraObjectsForTenantMode_RouteSvcMatches(t *testing.T) {
	objs, err := BuildGateway(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Gates: configv1.FeatureGates{
			LokiStackGateway: true,
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
			},
			expectedArgs: []string{
				"--tls.min-version=min-version",
				"--tls.cipher-suites=cipher1,cipher2",
			},
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			objs, err := BuildGateway(tc.options)
			require.NoError(t, err)

			d, ok := objs[1].(*appsv1.Deployment)
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
			},
			wantArgs: []string{
				"--logs.rules.endpoint=https://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
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
			},
			wantArgs: []string{
				"--logs.rules.endpoint=https://abcd-ruler-http.efgh.svc.cluster.local:3100",
				"--logs.rules.read-only=true",
			},
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			objs, err := BuildGateway(tc.opts)
			require.NoError(t, err)

			d, ok := objs[1].(*appsv1.Deployment)
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
