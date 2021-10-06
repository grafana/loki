package manifests

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal/gateway"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestApplyGatewayDefaultsOptions(t *testing.T) {
	type tt struct {
		desc string
		opts *Options
		want *Options
	}

	tc := []tt{
		{
			desc: "static mode",
			opts: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.Static,
					},
				},
			},
			want: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.Static,
					},
				},
			},
		},
		{
			desc: "dynamic mode",
			opts: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.Dynamic,
					},
				},
			},
			want: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.Dynamic,
					},
				},
			},
		},
		{
			desc: "openshift-logging mode",
			opts: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.OpenshiftLogging,
					},
				},
			},
			want: &Options{
				Stack: lokiv1beta1.LokiStackSpec{
					Tenants: &lokiv1beta1.TenantsSpec{
						Mode: lokiv1beta1.OpenshiftLogging,
						Authentication: []lokiv1beta1.AuthenticationSpec{
							{
								TenantName: "application",
								TenantID:   "",
								OIDC: &lokiv1beta1.OIDCSpec{
									IssuerURL:     "https://127.0.0.1:5556/dex",
									RedirectURL:   "http://localhost:8080/oidc/application/callback",
									UsernameClaim: "name",
								},
							},
							{
								TenantName: "infrastructure",
								TenantID:   "",
								OIDC: &lokiv1beta1.OIDCSpec{
									IssuerURL:     "https://127.0.0.1:5556/dex",
									RedirectURL:   "http://localhost:8080/oidc/infrastructure/callback",
									UsernameClaim: "name",
								},
							},
							{
								TenantName: "audit",
								TenantID:   "",
								OIDC: &lokiv1beta1.OIDCSpec{
									IssuerURL:     "https://127.0.0.1:5556/dex",
									RedirectURL:   "http://localhost:8080/oidc/audit/callback",
									UsernameClaim: "name",
								},
							},
						},
						Authorization: &lokiv1beta1.AuthorizationSpec{
							OPA: &lokiv1beta1.OPASpec{
								URL: "http://localhost:8082/data/lokistack/allow",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyGatewayDefaultOptions(tc.opts)
			require.NoError(t, err)

			for i, a := range tc.opts.Stack.Tenants.Authentication {
				a.TenantID = ""
				tc.opts.Stack.Tenants.Authentication[i] = a
			}

			require.Equal(t, tc.want, tc.opts)
		})
	}
}

func TestConfigureDeploymentForMode(t *testing.T) {
	type tt struct {
		desc  string
		mode  lokiv1beta1.ModeType
		flags FeatureFlags
		dpl   *appsv1.DeploymentSpec
		want  *appsv1.DeploymentSpec
	}

	tc := []tt{
		{
			desc: "static mode",
			mode: lokiv1beta1.Static,
			dpl:  &appsv1.DeploymentSpec{},
			want: &appsv1.DeploymentSpec{},
		},
		{
			desc: "dynamic mode",
			mode: lokiv1beta1.Dynamic,
			dpl:  &appsv1.DeploymentSpec{},
			want: &appsv1.DeploymentSpec{},
		},
		{
			desc: "openshift-logging mode",
			mode: lokiv1beta1.OpenshiftLogging,
			dpl:  &appsv1.DeploymentSpec{},
			want: &appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "opa-openshift",
								Image: "quay.io/observatorium/opa-openshift:latest",
								Args: []string{
									"--log.level=warn",
									"--opa.package=lokistack",
									"--web.listen=:8082",
									"--web.internal.listen=:8083",
									"--web.healthchecks.url=http://localhost:8082",
									`--openshift.mappings=application=loki.openshift.io`,
									`--openshift.mappings=infrastructure=loki.openshift.io`,
									`--openshift.mappings=audit=loki.openshift.io`,
								},
								Ports: []corev1.ContainerPort{
									{
										Name:          gatewayOPAHTTPPortName,
										ContainerPort: gatewayOPAHTTPPort,
										Protocol:      corev1.ProtocolTCP,
									},
									{
										Name:          gatewayOPAInternalPortName,
										ContainerPort: gatewayOPAInternalPort,
										Protocol:      corev1.ProtocolTCP,
									},
								},
								LivenessProbe: &corev1.Probe{
									Handler: corev1.Handler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/live",
											Port:   intstr.FromInt(gatewayOPAInternalPort),
											Scheme: corev1.URISchemeHTTP,
										},
									},
									TimeoutSeconds:   2,
									PeriodSeconds:    30,
									FailureThreshold: 10,
								},
								ReadinessProbe: &corev1.Probe{
									Handler: corev1.Handler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/ready",
											Port:   intstr.FromInt(gatewayOPAInternalPort),
											Scheme: corev1.URISchemeHTTP,
										},
									},
									TimeoutSeconds:   1,
									PeriodSeconds:    5,
									FailureThreshold: 12,
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode with-tls-service-monitor-config",
			mode: lokiv1beta1.OpenshiftLogging,
			flags: FeatureFlags{
				EnableTLSServiceMonitorConfig: true,
			},
			dpl: &appsv1.DeploymentSpec{},
			want: &appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "opa-openshift",
								Image: "quay.io/observatorium/opa-openshift:latest",
								Args: []string{
									"--log.level=warn",
									"--opa.package=lokistack",
									"--web.listen=:8082",
									"--web.internal.listen=:8083",
									"--web.healthchecks.url=http://localhost:8082",
									"--tls.internal.server.cert-file=/var/run/tls/tls.crt",
									"--tls.internal.server.key-file=/var/run/tls/tls.key",
									`--openshift.mappings=application=loki.openshift.io`,
									`--openshift.mappings=infrastructure=loki.openshift.io`,
									`--openshift.mappings=audit=loki.openshift.io`,
								},
								Ports: []corev1.ContainerPort{
									{
										Name:          gatewayOPAHTTPPortName,
										ContainerPort: gatewayOPAHTTPPort,
										Protocol:      corev1.ProtocolTCP,
									},
									{
										Name:          gatewayOPAInternalPortName,
										ContainerPort: gatewayOPAInternalPort,
										Protocol:      corev1.ProtocolTCP,
									},
								},
								LivenessProbe: &corev1.Probe{
									Handler: corev1.Handler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/live",
											Port:   intstr.FromInt(gatewayOPAInternalPort),
											Scheme: corev1.URISchemeHTTPS,
										},
									},
									TimeoutSeconds:   2,
									PeriodSeconds:    30,
									FailureThreshold: 10,
								},
								ReadinessProbe: &corev1.Probe{
									Handler: corev1.Handler{
										HTTPGet: &corev1.HTTPGetAction{
											Path:   "/ready",
											Port:   intstr.FromInt(gatewayOPAInternalPort),
											Scheme: corev1.URISchemeHTTPS,
										},
									},
									TimeoutSeconds:   1,
									PeriodSeconds:    5,
									FailureThreshold: 12,
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      tlsMetricsSercetVolume,
										ReadOnly:  true,
										MountPath: gateway.LokiGatewayTLSDir,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureDeploymentForMode(tc.dpl, tc.mode, tc.flags)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureServiceForMode(t *testing.T) {
	type tt struct {
		desc string
		mode lokiv1beta1.ModeType
		svc  *corev1.ServiceSpec
		want *corev1.ServiceSpec
	}

	tc := []tt{
		{
			desc: "static mode",
			mode: lokiv1beta1.Static,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{},
		},
		{
			desc: "dynamic mode",
			mode: lokiv1beta1.Dynamic,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{},
		},
		{
			desc: "openshift-logging mode",
			mode: lokiv1beta1.OpenshiftLogging,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: opaMetricsPortName,
						Port: gatewayOPAInternalPort,
					},
				},
			},
		},
	}
	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureServiceForMode(tc.svc, tc.mode)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.svc)
		})
	}
}

func TestConfigureServiceMonitorForMode(t *testing.T) {
	type tt struct {
		desc  string
		mode  lokiv1beta1.ModeType
		flags FeatureFlags
		sm    *monitoringv1.ServiceMonitor
		want  *monitoringv1.ServiceMonitor
	}

	tc := []tt{
		{
			desc: "static mode",
			mode: lokiv1beta1.Static,
			sm:   &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{},
		},
		{
			desc: "dynamic mode",
			mode: lokiv1beta1.Dynamic,
			sm:   &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{},
		},
		{
			desc: "openshift-logging mode",
			mode: lokiv1beta1.OpenshiftLogging,
			sm:   &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:   opaMetricsPortName,
							Path:   "/metrics",
							Scheme: "http",
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode with-tls-service-monitor-config",
			mode: lokiv1beta1.OpenshiftLogging,
			flags: FeatureFlags{
				EnableTLSServiceMonitorConfig: true,
			},
			sm: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							TLSConfig: &monitoringv1.TLSConfig{
								CAFile:   "/path/to/ca/file",
								CertFile: "/path/to/cert/file",
								KeyFile:  "/path/to/key/file",
							},
						},
					},
				},
			},
			want: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							TLSConfig: &monitoringv1.TLSConfig{
								CAFile:   "/path/to/ca/file",
								CertFile: "/path/to/cert/file",
								KeyFile:  "/path/to/key/file",
							},
						},
						{
							Port:            opaMetricsPortName,
							Path:            "/metrics",
							Scheme:          "https",
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								CAFile:   "/path/to/ca/file",
								CertFile: "/path/to/cert/file",
								KeyFile:  "/path/to/key/file",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureServiceMonitorForMode(tc.sm, tc.mode, tc.flags)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sm)
		})
	}
}
