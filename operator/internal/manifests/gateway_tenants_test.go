package manifests

import (
	"path"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

func defaultGatewayDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: gatewayContainerName,
						},
					},
				},
			},
		},
	}
}

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
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
			want: &Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
		},
		{
			desc: "static mode on openshift",
			opts: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
			},
			want: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						LokiStackName:        "lokistack-ocp",
						LokiStackNamespace:   "stack-ns",
						GatewayName:          "lokistack-ocp-gateway",
						GatewaySvcName:       "lokistack-ocp-gateway-http",
						GatewaySvcTargetPort: "public",
						GatewayRouteTimeout:  75 * time.Second,
						RulerName:            "lokistack-ocp-ruler",
						Labels:               ComponentLabels(LabelGatewayComponent, "lokistack-ocp"),
					},
				},
			},
		},
		{
			desc: "dynamic mode",
			opts: &Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
			},
			want: &Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
			},
		},
		{
			desc: "dynamic mode on openshift",
			opts: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
			},
			want: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						LokiStackName:        "lokistack-ocp",
						LokiStackNamespace:   "stack-ns",
						GatewayName:          "lokistack-ocp-gateway",
						GatewaySvcName:       "lokistack-ocp-gateway-http",
						GatewaySvcTargetPort: "public",
						GatewayRouteTimeout:  75 * time.Second,
						RulerName:            "lokistack-ocp-ruler",
						Labels:               ComponentLabels(LabelGatewayComponent, "lokistack-ocp"),
					},
				},
			},
		},
		{
			desc: "openshift-logging mode",
			opts: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				Tenants: Tenants{
					Configs: map[string]TenantConfig{
						"application": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "D31SJpSmPe6aUDTtU2zqAoW1gqEKoH5T",
							},
						},
						"infrastructure": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "i3N1paUy9JwNZIktni4kqXPuMvIHtHNe",
							},
						},
						"audit": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "6UssDXle7OHElqSW4M0DNRZ6JbaTjDM3",
							},
						},
					},
				},
			},
			want: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				Tenants: Tenants{
					Configs: map[string]TenantConfig{
						"application": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "D31SJpSmPe6aUDTtU2zqAoW1gqEKoH5T",
							},
						},
						"infrastructure": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "i3N1paUy9JwNZIktni4kqXPuMvIHtHNe",
							},
						},
						"audit": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "6UssDXle7OHElqSW4M0DNRZ6JbaTjDM3",
							},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						LokiStackName:        "lokistack-ocp",
						LokiStackNamespace:   "stack-ns",
						GatewayName:          "lokistack-ocp-gateway",
						GatewaySvcName:       "lokistack-ocp-gateway-http",
						GatewaySvcTargetPort: "public",
						GatewayRouteTimeout:  75 * time.Second,
						RulerName:            "lokistack-ocp-ruler",
						Labels:               ComponentLabels(LabelGatewayComponent, "lokistack-ocp"),
					},
					Authentication: []openshift.AuthenticationSpec{
						{
							TenantName:     "application",
							TenantID:       "",
							ServiceAccount: "lokistack-ocp-gateway",
							RedirectURL:    "https://lokistack-ocp-stack-ns.apps.example.com/openshift/application/callback",
						},
						{
							TenantName:     "infrastructure",
							TenantID:       "",
							ServiceAccount: "lokistack-ocp-gateway",
							RedirectURL:    "https://lokistack-ocp-stack-ns.apps.example.com/openshift/infrastructure/callback",
						},
						{
							TenantName:     "audit",
							TenantID:       "",
							ServiceAccount: "lokistack-ocp-gateway",
							RedirectURL:    "https://lokistack-ocp-stack-ns.apps.example.com/openshift/audit/callback",
						},
					},
					Authorization: openshift.AuthorizationSpec{
						OPAUrl: "http://localhost:8082/v1/data/lokistack/allow",
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			opts: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				Tenants: Tenants{
					Configs: map[string]TenantConfig{
						"network": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "hlVzbIVKMeeZTxNrHMb4hV2aVwAA4bte",
							},
						},
					},
				},
			},
			want: &Options{
				Name:              "lokistack-ocp",
				Namespace:         "stack-ns",
				GatewayBaseDomain: "example.com",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Timeouts: TimeoutConfig{
					Gateway: GatewayTimeoutConfig{
						WriteTimeout: 1 * time.Minute,
					},
				},
				Tenants: Tenants{
					Configs: map[string]TenantConfig{
						"network": {
							OpenShift: &TenantOpenShiftSpec{
								CookieSecret: "hlVzbIVKMeeZTxNrHMb4hV2aVwAA4bte",
							},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						LokiStackName:        "lokistack-ocp",
						LokiStackNamespace:   "stack-ns",
						GatewayName:          "lokistack-ocp-gateway",
						GatewaySvcName:       "lokistack-ocp-gateway-http",
						GatewaySvcTargetPort: "public",
						GatewayRouteTimeout:  75 * time.Second,
						RulerName:            "lokistack-ocp-ruler",
						Labels:               ComponentLabels(LabelGatewayComponent, "lokistack-ocp"),
					},
					Authentication: []openshift.AuthenticationSpec{
						{
							TenantName:     "network",
							TenantID:       "",
							ServiceAccount: "lokistack-ocp-gateway",
							RedirectURL:    "https://lokistack-ocp-stack-ns.apps.example.com/openshift/network/callback",
						},
					},
					Authorization: openshift.AuthorizationSpec{
						OPAUrl: "http://localhost:8082/v1/data/lokistack/allow",
					},
				},
			},
		},
	}
	for _, tc := range tc {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ApplyGatewayDefaultOptions(tc.opts)
			require.NoError(t, err)

			for i, a := range tc.opts.OpenShiftOptions.Authentication {
				require.NotEmpty(t, a.TenantID)
				require.NotEmpty(t, a.CookieSecret)
				require.Len(t, a.CookieSecret, 32)

				a.TenantID = ""
				a.CookieSecret = ""
				tc.opts.OpenShiftOptions.Authentication[i] = a
				tc.opts.OpenShiftOptions.Authentication[i] = a
			}

			require.Equal(t, tc.want, tc.opts)
		})
	}
}

func TestConfigureDeploymentForMode(t *testing.T) {
	type tt struct {
		desc         string
		stackName    string
		stackNs      string
		featureGates configv1.FeatureGates
		adminGroups  []string
		tenants      *lokiv1.TenantsSpec
		dpl          *appsv1.Deployment
		want         *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "static mode without tenants",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Static,
			},
			dpl:  defaultGatewayDeployment(),
			want: defaultGatewayDeployment(),
		},
		{
			desc: "dynamic mode",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Dynamic,
			},
			dpl:  defaultGatewayDeployment(),
			want: defaultGatewayDeployment(),
		},
		{
			desc:        "static mode with mTLS tenant configured",
			stackName:   "test",
			stackNs:     "test-ns",
			adminGroups: defaultAdminGroups,
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Static,
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test-a",
						TenantID:   "a",
						MTLS: &lokiv1.MTLSSpec{
							CA: &lokiv1.CASpec{
								CA:    "my-ca",
								CAKey: "my-ca-key",
							},
						},
					},
				},
			},
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{"--tls.client-auth-type=NoClientCert"},
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{"--tls.client-auth-type=RequestClientCert"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test-a-ca-bundle",
											MountPath: "/var/run/tenants-ca/test-a",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test-a-ca-bundle",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "my-ca",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc:      "dynamic mode with mTLS tenant configured",
			stackName: "test",
			stackNs:   "test-ns",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Dynamic,
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test-a",
						TenantID:   "a",
						MTLS: &lokiv1.MTLSSpec{
							CA: &lokiv1.CASpec{
								CA:    "my-ca",
								CAKey: "my-ca-key",
							},
						},
					},
				},
			},
			dpl: defaultGatewayDeployment(),
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{"--tls.client-auth-type=RequestClientCert"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test-a-ca-bundle",
											MountPath: "/var/run/tenants-ca/test-a",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test-a-ca-bundle",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "my-ca",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc:      "static mode with OIDC IssuerCA configured",
			stackName: "test",
			stackNs:   "test-ns",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Static,
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test-a",
						TenantID:   "a",
						OIDC: &lokiv1.OIDCSpec{
							IssuerCA: &lokiv1.CASpec{
								CA:    "my-ca",
								CAKey: "my-ca-key",
							},
						},
					},
				},
			},
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{"--tls.client-auth-type=NoClientCert"},
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{"--tls.client-auth-type=NoClientCert"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test-a-ca-bundle",
											MountPath: "/var/run/tenants-ca/test-a",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test-a-ca-bundle",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "my-ca",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc:      "dynamic mode with mTLS tenant configured",
			stackName: "test",
			stackNs:   "test-ns",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.Dynamic,
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test-a",
						TenantID:   "a",
						OIDC: &lokiv1.OIDCSpec{
							IssuerCA: &lokiv1.CASpec{
								CA:    "my-ca",
								CAKey: "my-ca-key",
							},
						},
					},
				},
			},
			dpl: defaultGatewayDeployment(),
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test-a-ca-bundle",
											MountPath: "/var/run/tenants-ca/test-a",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test-a-ca-bundle",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "my-ca",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
			stackName:   "test",
			stackNs:     "test-ns",
			dpl:         defaultGatewayDeployment(),
			adminGroups: defaultAdminGroups,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{
										"--logs.auth.extract-selectors=kubernetes_namespace_name",
									},
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.admin-groups=system:cluster-admins,cluster-admin,dedicated-admin",
										"--opa.matcher=kubernetes_namespace_name",
										`--openshift.mappings=application=loki.grafana.com`,
										`--openshift.mappings=infrastructure=loki.grafana.com`,
										`--openshift.mappings=audit=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
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
		},
		{
			desc: "openshift-logging mode with http encryption",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
			stackName: "test",
			stackNs:   "test-ns",
			featureGates: configv1.FeatureGates{
				HTTPEncryption:             true,
				ServiceMonitorTLSEndpoints: true,
			},
			adminGroups: defaultAdminGroups,
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							ServiceAccountName: "test-gateway",
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{
										"--logs.auth.extract-selectors=kubernetes_namespace_name",
									},
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.admin-groups=system:cluster-admins,cluster-admin,dedicated-admin",
										"--opa.matcher=kubernetes_namespace_name",
										"--tls.internal.server.cert-file=/var/run/tls/http/server/tls.crt",
										"--tls.internal.server.key-file=/var/run/tls/http/server/tls.key",
										"--tls.min-version=min-version",
										"--tls.cipher-suites=cipher1,cipher2",
										`--openshift.mappings=application=loki.grafana.com`,
										`--openshift.mappings=infrastructure=loki.grafana.com`,
										`--openshift.mappings=audit=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTPS,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTPS,
											},
										},
										TimeoutSeconds:   1,
										PeriodSeconds:    5,
										FailureThreshold: 12,
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      tlsSecretVolume,
											ReadOnly:  true,
											MountPath: gatewayServerHTTPTLSDir(),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftNetwork,
			},
			stackName:   "test",
			stackNs:     "test-ns",
			adminGroups: defaultAdminGroups,
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.admin-groups=system:cluster-admins,cluster-admin,dedicated-admin",
										"--opa.matcher=SrcK8S_Namespace,DstK8S_Namespace",
										"--opa.matcher-op=or",
										`--openshift.mappings=network=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										TimeoutSeconds:   1,
										PeriodSeconds:    5,
										FailureThreshold: 12,
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode with http encryption",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftNetwork,
			},
			stackName: "test",
			stackNs:   "test-ns",
			featureGates: configv1.FeatureGates{
				HTTPEncryption:             true,
				ServiceMonitorTLSEndpoints: true,
			},
			adminGroups: defaultAdminGroups,
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.admin-groups=system:cluster-admins,cluster-admin,dedicated-admin",
										"--opa.matcher=SrcK8S_Namespace,DstK8S_Namespace",
										"--opa.matcher-op=or",
										"--tls.internal.server.cert-file=/var/run/tls/http/server/tls.crt",
										"--tls.internal.server.key-file=/var/run/tls/http/server/tls.key",
										"--tls.min-version=min-version",
										"--tls.cipher-suites=cipher1,cipher2",
										`--openshift.mappings=network=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTPS,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTPS,
											},
										},
										TimeoutSeconds:   1,
										PeriodSeconds:    5,
										FailureThreshold: 12,
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      tlsSecretVolume,
											ReadOnly:  true,
											MountPath: path.Join(httpTLSDir, "server"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "tls-secret-volume",
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode with custom admin group list",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
			stackName: "test",
			stackNs:   "test-ns",
			adminGroups: []string{
				"custom-admins",
				"other-admins",
			},
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{
										"--logs.auth.extract-selectors=kubernetes_namespace_name",
									},
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.admin-groups=custom-admins,other-admins",
										"--opa.matcher=kubernetes_namespace_name",
										`--openshift.mappings=application=loki.grafana.com`,
										`--openshift.mappings=infrastructure=loki.grafana.com`,
										`--openshift.mappings=audit=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
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
		},
		{
			desc: "openshift-logging mode with empty admin group list",
			tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
			stackName:   "test",
			stackNs:     "test-ns",
			adminGroups: []string{},
			dpl: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: gatewayContainerName,
									Args: []string{
										"--logs.auth.extract-selectors=kubernetes_namespace_name",
									},
								},
								{
									Name:  "opa",
									Image: "quay.io/observatorium/opa-openshift:latest",
									Args: []string{
										"--log.level=warn",
										"--web.listen=:8082",
										"--web.internal.listen=:8083",
										"--web.healthchecks.url=http://localhost:8082",
										"--opa.skip-tenants=audit,infrastructure",
										"--opa.package=lokistack",
										"--opa.matcher=kubernetes_namespace_name",
										`--openshift.mappings=application=loki.grafana.com`,
										`--openshift.mappings=infrastructure=loki.grafana.com`,
										`--openshift.mappings=audit=loki.grafana.com`,
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          openshift.GatewayOPAHTTPPortName,
											ContainerPort: openshift.GatewayOPAHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          openshift.GatewayOPAInternalPortName,
											ContainerPort: openshift.GatewayOPAInternalPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/live",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										TimeoutSeconds:   2,
										PeriodSeconds:    30,
										FailureThreshold: 10,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/ready",
												Port:   intstr.FromInt(int(openshift.GatewayOPAInternalPort)),
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
		},
	}

	for _, tc := range tc {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureGatewayDeploymentForMode(tc.dpl, tc.tenants, tc.featureGates, "min-version", "cipher1,cipher2", tc.adminGroups)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureServiceForMode(t *testing.T) {
	type tt struct {
		desc string
		mode lokiv1.ModeType
		svc  *corev1.ServiceSpec
		want *corev1.ServiceSpec
	}

	tc := []tt{
		{
			desc: "static mode",
			mode: lokiv1.Static,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{},
		},
		{
			desc: "dynamic mode",
			mode: lokiv1.Dynamic,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{},
		},
		{
			desc: "openshift-logging mode",
			mode: lokiv1.OpenshiftLogging,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: openshift.GatewayOPAInternalPortName,
						Port: openshift.GatewayOPAInternalPort,
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			mode: lokiv1.OpenshiftNetwork,
			svc:  &corev1.ServiceSpec{},
			want: &corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: openshift.GatewayOPAInternalPortName,
						Port: openshift.GatewayOPAInternalPort,
					},
				},
			},
		},
	}
	for _, tc := range tc {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureGatewayServiceForMode(tc.svc, tc.mode)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.svc)
		})
	}
}

func TestConfigureServiceMonitorForMode(t *testing.T) {
	type tt struct {
		desc string
		opts Options
		mode lokiv1.ModeType
		sm   *monitoringv1.ServiceMonitor
		want *monitoringv1.ServiceMonitor
	}

	tc := []tt{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
			sm:   &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{},
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
			},
			sm:   &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{},
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
			},
			sm: &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:   openshift.GatewayOPAInternalPortName,
							Path:   "/metrics",
							Scheme: "http",
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
			},
			sm: &monitoringv1.ServiceMonitor{},
			want: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:   openshift.GatewayOPAInternalPortName,
							Path:   "/metrics",
							Scheme: "http",
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode with-tls-service-monitor-config",
			opts: Options{
				Name:      "abcd",
				Namespace: "ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Gates: configv1.FeatureGates{
					HTTPEncryption:             true,
					ServiceMonitorTLSEndpoints: true,
				},
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
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
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
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
							},
						},
						{
							Port:   openshift.GatewayOPAInternalPortName,
							Path:   "/metrics",
							Scheme: "https",
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
							},
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
		{
			desc: "openshift-network mode with-tls-service-monitor-config",
			mode: lokiv1.OpenshiftNetwork,
			opts: Options{
				Name:      "abcd",
				Namespace: "ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Gates: configv1.FeatureGates{
					HTTPEncryption:             true,
					ServiceMonitorTLSEndpoints: true,
				},
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
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
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
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
							},
						},
						{
							Port:   openshift.GatewayOPAInternalPortName,
							Path:   "/metrics",
							Scheme: "https",
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "abcd-gateway-token",
									},
									Key: corev1.ServiceAccountTokenKey,
								},
							},
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
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := configureGatewayServiceMonitorForMode(tc.sm, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sm)
		})
	}
}
