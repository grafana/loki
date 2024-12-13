package manifests

import (
	"fmt"
	"testing"

	"github.com/ViaQ/logerr/v2/kverrors"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
)

func TestApplyUserOptions_OverrideDefaults(t *testing.T) {
	allSizes := []lokiv1.LokiStackSizeType{
		lokiv1.SizeOneXDemo,
		lokiv1.SizeOneXExtraSmall,
		lokiv1.SizeOneXSmall,
		lokiv1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1.LokiStackSpec{
				Size: size,
				Template: &lokiv1.LokiTemplateSpec{
					Distributor: &lokiv1.LokiComponentSpec{
						Replicas: 42,
					},
				},
			},
			Timeouts: defaultTimeoutConfig,
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)
		require.Equal(t, defs.Size, opt.Stack.Size)
		require.Equal(t, defs.Limits, opt.Stack.Limits)
		require.Equal(t, defs.ReplicationFactor, opt.Stack.ReplicationFactor) //nolint:staticcheck
		require.Equal(t, defs.Replication, opt.Stack.Replication)
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
	allSizes := []lokiv1.LokiStackSizeType{
		lokiv1.SizeOneXDemo,
		lokiv1.SizeOneXExtraSmall,
		lokiv1.SizeOneXSmall,
		lokiv1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1.LokiStackSpec{
				Size: size,
				Template: &lokiv1.LokiTemplateSpec{
					Compactor: &lokiv1.LokiComponentSpec{
						Replicas: 2,
					},
				},
			},
			Timeouts: defaultTimeoutConfig,
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)

		// Require compactor to be reverted to 1 replica
		require.Equal(t, defs.Template.Compactor, opt.Stack.Template.Compactor)
	}
}

func TestApplyTLSSettings_OverrideDefaults(t *testing.T) {
	type tt struct {
		desc     string
		profile  openshiftconfigv1.TLSSecurityProfile
		expected TLSProfileSpec
		err      error
	}

	tc := []tt{
		{
			desc: "Old profile",
			profile: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileOldType,
			},
			expected: TLSProfileSpec{
				MinTLSVersion: "VersionTLS10",
				Ciphers: []string{
					"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
					"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
					"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
					"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
					"TLS_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_RSA_WITH_AES_256_GCM_SHA384",
					"TLS_RSA_WITH_AES_128_CBC_SHA256",
					"TLS_RSA_WITH_AES_128_CBC_SHA",
					"TLS_RSA_WITH_AES_256_CBC_SHA",
					"TLS_RSA_WITH_3DES_EDE_CBC_SHA",
				},
			},
		},
		{
			desc: "Intermediate profile",
			profile: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileIntermediateType,
			},
			expected: TLSProfileSpec{
				MinTLSVersion: "VersionTLS12",
				Ciphers: []string{
					"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				},
			},
		},
		{
			desc: "Modern profile",
			profile: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileModernType,
			},
			expected: TLSProfileSpec{
				MinTLSVersion: "VersionTLS13",
				// Go lib crypto doesn't allow ciphers to be configured for TLS 1.3
				// (Read this and weep: https://github.com/golang/go/issues/29349)
				Ciphers: []string{},
			},
		},
		{
			desc: "custom profile",
			profile: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileCustomType,
				Custom: &openshiftconfigv1.CustomTLSProfile{
					TLSProfileSpec: openshiftconfigv1.TLSProfileSpec{
						MinTLSVersion: "VersionTLS11",
						Ciphers: []string{
							"ECDHE-ECDSA-CHACHA20-POLY1305",
							"ECDHE-RSA-CHACHA20-POLY1305",
							"ECDHE-RSA-AES128-GCM-SHA256",
							"ECDHE-ECDSA-AES128-GCM-SHA256",
						},
					},
				},
			},
			expected: TLSProfileSpec{
				MinTLSVersion: "VersionTLS11",
				Ciphers: []string{
					"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				},
			},
		},
		{
			desc: "broken custom profile",
			profile: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileCustomType,
			},
			err: kverrors.New("missing TLS custom profile spec"),
		},
	}

	for _, tc := range tc {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			opts := Options{}
			err := ApplyTLSSettings(&opts, &tc.profile)

			require.EqualValues(t, tc.err, err)
			require.EqualValues(t, tc.expected, opts.TLSProfile)
		})
	}
}

func TestBuildAll_WithFeatureGates_ServiceMonitors(t *testing.T) {
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
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors:            false,
					ServiceMonitorTLSEndpoints: false,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: false,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc:         "service monitor per component created",
			MonitorCount: 8,
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors:            true,
					ServiceMonitorTLSEndpoints: false,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: false,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}

	for _, tst := range table {
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

func TestBuildAll_WithFeatureGates_OpenShift_ServingCertsService(t *testing.T) {
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
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors:            false,
					ServiceMonitorTLSEndpoints: false,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: false,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc: "enabled certificate signing service for every http and grpc service",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors:            false,
					ServiceMonitorTLSEndpoints: false,
					OpenShift: configv1.OpenShiftFeatureGates{
						ServingCertsService: true,
					},
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}

	for _, tst := range table {
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			svcs := []*corev1.Service{
				NewGatewayHTTPService(tst.BuildOptions),
			}

			for _, service := range svcs {
				if !tst.BuildOptions.Gates.OpenShift.ServingCertsService {
					require.Equal(t, service.ObjectMeta.Annotations, map[string]string{})
				} else {
					require.NotNil(t, service.ObjectMeta.Annotations["service.beta.openshift.io/serving-cert-secret-name"])
				}
			}
		})
	}
}

func TestBuildAll_WithFeatureGates_HTTPEncryption(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXSmall,
			Rules: &lokiv1.RulesSpec{
				Enabled: true,
			},
		},
		Gates: configv1.FeatureGates{
			HTTPEncryption: true,
		},
		Timeouts: defaultTimeoutConfig,
	}

	err := ApplyDefaultSettings(&opts)
	require.NoError(t, err)
	err = ApplyTLSSettings(&opts, nil)
	require.NoError(t, err)
	objects, buildErr := BuildAll(opts)
	require.NoError(t, buildErr)

	for _, obj := range objects {
		var (
			name string
			vs   []corev1.Volume
			vms  []corev1.VolumeMount
			rps  corev1.URIScheme
			lps  corev1.URIScheme
		)

		switch o := obj.(type) {
		case *appsv1.Deployment:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		case *appsv1.StatefulSet:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		default:
			continue
		}

		secretName := fmt.Sprintf("%s-http", name)
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
			MountPath: "/var/run/tls/http/server",
		}
		require.Contains(t, vms, expVolumeMount)

		require.Equal(t, corev1.URISchemeHTTPS, rps)
		require.Equal(t, corev1.URISchemeHTTPS, lps)
	}
}

func TestBuildAll_WithFeatureGates_ServiceMonitorTLSEndpoints(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXSmall,
			Rules: &lokiv1.RulesSpec{
				Enabled: true,
			},
		},
		Gates: configv1.FeatureGates{
			ServiceMonitors:            true,
			HTTPEncryption:             true,
			ServiceMonitorTLSEndpoints: true,
		},
		Timeouts: defaultTimeoutConfig,
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
			rps  corev1.URIScheme
			lps  corev1.URIScheme
		)

		switch o := obj.(type) {
		case *appsv1.Deployment:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		case *appsv1.StatefulSet:
			name = o.Name
			vs = o.Spec.Template.Spec.Volumes
			vms = o.Spec.Template.Spec.Containers[0].VolumeMounts
			rps = o.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme
			lps = o.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme
		default:
			continue
		}

		secretName := fmt.Sprintf("%s-http", name)
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
			MountPath: "/var/run/tls/http/server",
		}
		require.Contains(t, vms, expVolumeMount)

		require.Equal(t, corev1.URISchemeHTTPS, rps)
		require.Equal(t, corev1.URISchemeHTTPS, lps)
	}
}

func TestBuildAll_WithFeatureGates_GRPCEncryption(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}

	table := []test{
		{
			desc: "disabled grpc over tls services",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
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
				Gates: configv1.FeatureGates{
					GRPCEncryption: false,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc: "enabled grpc over tls services",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
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
				Gates: configv1.FeatureGates{
					GRPCEncryption: true,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}

	secretsMap := map[string]string{
		// deployments
		"test-distributor":    "test-distributor-grpc",
		"test-querier":        "test-querier-grpc",
		"test-query-frontend": "test-query-frontend-grpc",
		// statefulsets
		"test-ingester":      "test-ingester-grpc",
		"test-compactor":     "test-compactor-grpc",
		"test-index-gateway": "test-index-gateway-grpc",
		"test-ruler":         "test-ruler-grpc",
	}

	for _, tst := range table {
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			err = ApplyTLSSettings(&tst.BuildOptions, nil)
			require.NoError(t, err)

			objs, err := BuildAll(tst.BuildOptions)
			require.NoError(t, err)

			for _, o := range objs {
				var (
					name string
					spec *corev1.PodSpec
				)
				switch obj := o.(type) {
				case *appsv1.Deployment:
					name = obj.Name
					spec = &obj.Spec.Template.Spec
				case *appsv1.StatefulSet:
					name = obj.Name
					spec = &obj.Spec.Template.Spec
				default:
					continue
				}

				t.Run(name, func(t *testing.T) {
					secretName := secretsMap[name]

					vm := corev1.VolumeMount{
						Name:      secretName,
						ReadOnly:  false,
						MountPath: "/var/run/tls/grpc/server",
					}

					v := corev1.Volume{
						Name: secretName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: secretName,
							},
						},
					}

					if tst.BuildOptions.Gates.GRPCEncryption {
						require.Contains(t, spec.Containers[0].VolumeMounts, vm)
						require.Contains(t, spec.Volumes, v)
					} else {
						require.NotContains(t, spec.Containers[0].VolumeMounts, vm)
						require.NotContains(t, spec.Volumes, v)
					}
				})
			}
		})
	}
}

func TestBuildAll_WithFeatureGates_RestrictedPodSecurityStandard(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}

	table := []test{
		{
			desc: "disabled restricted security standard",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
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
				Gates: configv1.FeatureGates{
					RestrictedPodSecurityStandard: false,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc: "enabled restricted security standard",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
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
				Gates: configv1.FeatureGates{
					RestrictedPodSecurityStandard: true,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}

	for _, tst := range table {
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			objs, err := BuildAll(tst.BuildOptions)
			require.NoError(t, err)

			for _, o := range objs {
				var (
					name string
					spec *corev1.PodSpec
				)
				switch obj := o.(type) {
				case *appsv1.Deployment:
					name = obj.Name
					spec = &obj.Spec.Template.Spec
				case *appsv1.StatefulSet:
					name = obj.Name
					spec = &obj.Spec.Template.Spec
				default:
					continue
				}

				t.Run(name, func(t *testing.T) {
					if tst.BuildOptions.Gates.RestrictedPodSecurityStandard {
						require.NotNil(t, spec.SecurityContext)

						require.True(t, *spec.SecurityContext.RunAsNonRoot)

						require.NotNil(t, spec.SecurityContext.SeccompProfile)
						require.Equal(t, spec.SecurityContext.SeccompProfile.Type, corev1.SeccompProfileTypeRuntimeDefault)
					} else {
						require.Nil(t, spec.SecurityContext)
					}

					for _, c := range spec.Containers {
						if tst.BuildOptions.Gates.RestrictedPodSecurityStandard {
							require.False(t, *c.SecurityContext.AllowPrivilegeEscalation)

							require.Empty(t, c.SecurityContext.Capabilities.Add)
							require.Equal(t, c.SecurityContext.Capabilities.Drop, []corev1.Capability{"ALL"})
						} else {
							require.Nil(t, c.SecurityContext)
						}
					}
				})
			}
		})
	}
}

func TestBuildAll_WithFeatureGates_LokiStackGateway(t *testing.T) {
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
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway:           false,
					HTTPEncryption:             true,
					ServiceMonitorTLSEndpoints: false,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc: "lokistack-gateway created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
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
				Gates: configv1.FeatureGates{
					LokiStackGateway:           true,
					HTTPEncryption:             true,
					ServiceMonitorTLSEndpoints: true,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)
			objects, buildErr := BuildAll(tst.BuildOptions)
			require.NoError(t, buildErr)
			if tst.BuildOptions.Gates.LokiStackGateway {
				require.True(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			} else {
				require.False(t, checkGatewayDeployed(objects, tst.BuildOptions.Name))
			}
		})
	}
}

func TestBuildAll_WithFeatureGates_LokiStackAlerts(t *testing.T) {
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
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors: false,
					LokiStackAlerts: false,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
		{
			desc: "alerts created",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXSmall,
				},
				Gates: configv1.FeatureGates{
					ServiceMonitors: true,
					LokiStackAlerts: true,
				},
				Timeouts: defaultTimeoutConfig,
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)
			objects, buildErr := BuildAll(tst.BuildOptions)
			require.NoError(t, buildErr)
			if tst.BuildOptions.Gates.LokiStackGateway {
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
