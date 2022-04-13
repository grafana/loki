package manifests

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/stretchr/testify/require"
)

func TestApplyUserOptions_OverrideDefaults(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Distributor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 42,
					},
				},
			},
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)
		require.Equal(t, defs.Size, opt.Stack.Size)
		require.Equal(t, defs.Limits, opt.Stack.Limits)
		require.Equal(t, defs.ReplicationFactor, opt.Stack.ReplicationFactor)
		require.Equal(t, defs.ManagementState, opt.Stack.ManagementState)
		require.Equal(t, defs.Template.Ingester, opt.Stack.Template.Ingester)
		require.Equal(t, defs.Template.Querier, opt.Stack.Template.Querier)
		require.Equal(t, defs.Template.QueryFrontend, opt.Stack.Template.QueryFrontend)

		// Require distributor replicas to be set by user overwrite
		require.NotEqual(t, defs.Template.Distributor.Replicas, opt.Stack.Template.Distributor.Replicas)

		// Require distributor tolerations and nodeselectors to use defaults
		require.Equal(t, defs.Template.Distributor.Tolerations, opt.Stack.Template.Distributor.Tolerations)
		require.Equal(t, defs.Template.Distributor.NodeSelector, opt.Stack.Template.Distributor.NodeSelector)
	}
}

func TestApplyUserOptions_AlwaysSetCompactorReplicasToOne(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Compactor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 2,
					},
				},
			},
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)

		// Require compactor to be reverted to 1 replica
		require.Equal(t, defs.Template.Compactor, opt.Stack.Template.Compactor)
	}
}

func TestBuildAll_WithFeatureFlags_EnableServiceMonitors(t *testing.T) {
	type test struct {
		desc         string
		MonitorCount int
		BuildOptions Options
	}

	table := []test{
		{
			desc:         "no service monitors created",
			MonitorCount: 0,
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
					Rules: &lokiv1beta1.RulesSpec{
						Enabled: true,
					},
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
		{
			desc:         "service monitor per component created",
			MonitorCount: 8,
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           true,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
	}

	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			objects, buildErr := BuildAll(tst.BuildOptions)

			require.NoError(t, buildErr)
			require.Equal(t, tst.MonitorCount, serviceMonitorCount(objects))
		})
	}
}

func TestBuildAll_WithFeatureFlags_EnableCertificateSigningService(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}

	table := []test{
		{
			desc: "disabled certificate signing service",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
		{
			desc: "enabled certificate signing service for every http service",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: true,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
	}

	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			httpServices := []*corev1.Service{
				NewDistributorHTTPService(tst.BuildOptions),
				NewIngesterHTTPService(tst.BuildOptions),
				NewQuerierHTTPService(tst.BuildOptions),
				NewQueryFrontendHTTPService(tst.BuildOptions),
				NewCompactorHTTPService(tst.BuildOptions),
				NewIndexGatewayHTTPService(tst.BuildOptions),
				NewRulerHTTPService(tst.BuildOptions),
				NewGatewayHTTPService(tst.BuildOptions),
			}

			for _, service := range httpServices {
				if !tst.BuildOptions.Flags.EnableCertificateSigningService {
					require.Equal(t, service.ObjectMeta.Annotations, map[string]string{})
				} else {
					require.NotNil(t, service.ObjectMeta.Annotations["service.beta.openshift.io/serving-cert-secret-name"])
				}
			}
		})
	}
}

func TestBuildAll_WithFeatureFlags_EnableTLSServiceMonitorConfig(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test",
		Stack: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXSmall,
			Rules: &lokiv1beta1.RulesSpec{
				Enabled: true,
			},
		},
		Flags: FeatureFlags{
			EnableServiceMonitors:         true,
			EnableTLSServiceMonitorConfig: true,
		},
	}

	err := ApplyDefaultSettings(&opts)
	require.NoError(t, err)
	objects, buildErr := BuildAll(opts)
	require.NoError(t, buildErr)
	require.Equal(t, 8, serviceMonitorCount(objects))

	for _, obj := range objects {
		var (
			name string
			vs   []corev1.Volume
			vms  []corev1.VolumeMount
			args []string
			rps  corev1.URIScheme
			lps  corev1.URIScheme
		)

		switch o := obj.(type) {
		case *appsv1.Deployment:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			args = o.Spec.Template.Spec.Containers[0].Args
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		case *appsv1.StatefulSet:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			args = o.Spec.Template.Spec.Containers[0].Args
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		default:
			continue
		}

		secretName := fmt.Sprintf("%s-http-metrics", name)
		expVolume := corev1.Volume{
			Name: secretName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		}
		require.Contains(t, vs, expVolume)

		expVolumeMount := corev1.VolumeMount{
			Name:      secretName,
			ReadOnly:  false,
			MountPath: "/etc/proxy/secrets",
		}
		require.Contains(t, vms, expVolumeMount)

		require.Contains(t, args, "-server.http-tls-cert-path=/etc/proxy/secrets/tls.crt")
		require.Contains(t, args, "-server.http-tls-key-path=/etc/proxy/secrets/tls.key")
		require.Equal(t, corev1.URISchemeHTTPS, rps)
		require.Equal(t, corev1.URISchemeHTTPS, lps)
	}
}

func TestBuildAll_WithFeatureFlags_EnableGateway(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}
	table := []test{
		{
			desc: "no lokistack-gateway created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableGateway:                 false,
					EnableTLSServiceMonitorConfig: false,
				},
			},
		},
		{
			desc: "lokistack-gateway created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
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
				Flags: FeatureFlags{
					EnableGateway:                 true,
					EnableTLSServiceMonitorConfig: true,
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)
			objects, buildErr := BuildAll(tst.BuildOptions)
			require.NoError(t, buildErr)
			if tst.BuildOptions.Flags.EnableGateway {
				require.True(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			} else {
				require.False(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			}
		})
	}
}

func TestBuildAll_WithFeatureFlags_EnablePrometheusAlerts(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}
	table := []test{
		{
			desc: "no alerts created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableServiceMonitors:  false,
					EnablePrometheusAlerts: false,
				},
			},
		},
		{
			desc: "alerts created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableServiceMonitors:  true,
					EnablePrometheusAlerts: true,
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)
			objects, buildErr := BuildAll(tst.BuildOptions)
			require.NoError(t, buildErr)
			if tst.BuildOptions.Flags.EnableGateway {
				require.True(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			} else {
				require.False(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			}
		})
	}
}

func serviceMonitorCount(objects []client.Object) int {
	monitors := 0
	for _, obj := range objects {
		if obj.GetObjectKind().GroupVersionKind().Kind == "ServiceMonitor" {
			monitors++
		}
	}
	return monitors
}

func checkGatewayDeployed(objects []client.Object, stackName string) bool {
	for _, obj := range objects {
		if obj.GetObjectKind().GroupVersionKind().Kind == "Deployment" &&
			obj.GetName() == GatewayName(stackName) {
			return true
		}
	}
	return false
}
