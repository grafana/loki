package manifests

import (
	"fmt"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

// Test that all serviceMonitor match the labels of their services so that we know all serviceMonitor
// will work when deployed.
func TestServiceMonitorMatchLabels(t *testing.T) {
	type test struct {
		Service        *corev1.Service
		ServiceMonitor *monitoringv1.ServiceMonitor
	}

	featureGates := configv1.FeatureGates{
		BuiltInCertManagement:      configv1.BuiltInCertManagement{Enabled: true},
		ServiceMonitors:            true,
		ServiceMonitorTLSEndpoints: true,
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Gates:     featureGates,
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
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
	}

	table := []test{
		{
			Service:        NewDistributorHTTPService(opt),
			ServiceMonitor: NewDistributorServiceMonitor(opt),
		},
		{
			Service:        NewIngesterHTTPService(opt),
			ServiceMonitor: NewIngesterServiceMonitor(opt),
		},
		{
			Service:        NewQuerierHTTPService(opt),
			ServiceMonitor: NewQuerierServiceMonitor(opt),
		},
		{
			Service:        NewQueryFrontendHTTPService(opt),
			ServiceMonitor: NewQueryFrontendServiceMonitor(opt),
		},
		{
			Service:        NewCompactorHTTPService(opt),
			ServiceMonitor: NewCompactorServiceMonitor(opt),
		},
		{
			Service:        NewGatewayHTTPService(opt),
			ServiceMonitor: NewGatewayServiceMonitor(opt),
		},
		{
			Service:        NewIndexGatewayHTTPService(opt),
			ServiceMonitor: NewIndexGatewayServiceMonitor(opt),
		},
		{
			Service:        NewRulerHTTPService(opt),
			ServiceMonitor: NewRulerServiceMonitor(opt),
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

func TestServiceMonitorEndpoints_ForBuiltInCertRotation(t *testing.T) {
	type test struct {
		Service        *corev1.Service
		ServiceMonitor *monitoringv1.ServiceMonitor
	}

	featureGates := configv1.FeatureGates{
		BuiltInCertManagement:      configv1.BuiltInCertManagement{Enabled: true},
		ServiceMonitors:            true,
		ServiceMonitorTLSEndpoints: true,
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Gates:     featureGates,
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
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
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}

	table := []test{
		{
			Service:        NewDistributorHTTPService(opt),
			ServiceMonitor: NewDistributorServiceMonitor(opt),
		},
		{
			Service:        NewIngesterHTTPService(opt),
			ServiceMonitor: NewIngesterServiceMonitor(opt),
		},
		{
			Service:        NewQuerierHTTPService(opt),
			ServiceMonitor: NewQuerierServiceMonitor(opt),
		},
		{
			Service:        NewQueryFrontendHTTPService(opt),
			ServiceMonitor: NewQueryFrontendServiceMonitor(opt),
		},
		{
			Service:        NewCompactorHTTPService(opt),
			ServiceMonitor: NewCompactorServiceMonitor(opt),
		},
		{
			Service:        NewIndexGatewayHTTPService(opt),
			ServiceMonitor: NewIndexGatewayServiceMonitor(opt),
		},
		{
			Service:        NewRulerHTTPService(opt),
			ServiceMonitor: NewRulerServiceMonitor(opt),
		},
	}

	for _, tst := range table {
		testName := fmt.Sprintf("%s_%s", tst.Service.GetName(), tst.ServiceMonitor.GetName())
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, tst.ServiceMonitor.Spec.Endpoints)
			require.NotNil(t, tst.ServiceMonitor.Spec.Endpoints[0].TLSConfig)

			// Do not use bearer authentication for loki endpoints
			require.Empty(t, tst.ServiceMonitor.Spec.Endpoints[0].BearerTokenFile)   //nolint:staticcheck
			require.Empty(t, tst.ServiceMonitor.Spec.Endpoints[0].BearerTokenSecret) //nolint:staticcheck

			// Check using built-in PKI
			c := tst.ServiceMonitor.Spec.Endpoints[0].TLSConfig
			require.Equal(t, c.CA.ConfigMap.LocalObjectReference.Name, signingCABundleName(opt.Name))
			require.Equal(t, c.Cert.Secret.LocalObjectReference.Name, tst.Service.Name)
			require.Equal(t, c.KeySecret.LocalObjectReference.Name, tst.Service.Name)
		})
	}
}

func TestServiceMonitorEndpoints_ForGatewayServiceMonitor(t *testing.T) {
	tt := []struct {
		desc  string
		opts  Options
		total int
		want  []monitoringv1.Endpoint
	}{
		{
			desc: "default",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 1,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "http",
				},
			},
		},
		{
			desc: "with http encryption",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Gates: configv1.FeatureGates{
					LokiStackGateway:           true,
					BuiltInCertManagement:      configv1.BuiltInCertManagement{Enabled: true},
					ServiceMonitors:            true,
					ServiceMonitorTLSEndpoints: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 1,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "https",
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-gateway-token",
							},
							Key: corev1.ServiceAccountTokenKey,
						},
					},
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gatewaySigningCABundleName("test-gateway"),
									},
									Key: caFile,
								},
							},
							ServerName: ptr.To("test-gateway-http.test.svc.cluster.local"),
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 2,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "http",
				},
				{
					Port:   "opa-metrics",
					Path:   "/metrics",
					Scheme: "http",
				},
			},
		},
		{
			desc: "openshift-logging with http encryption",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Gates: configv1.FeatureGates{
					LokiStackGateway:           true,
					BuiltInCertManagement:      configv1.BuiltInCertManagement{Enabled: true},
					ServiceMonitors:            true,
					ServiceMonitorTLSEndpoints: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 2,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "https",
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-gateway-token",
							},
							Key: corev1.ServiceAccountTokenKey,
						},
					},
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gatewaySigningCABundleName("test-gateway"),
									},
									Key: caFile,
								},
							},
							ServerName: ptr.To("test-gateway-http.test.svc.cluster.local"),
						},
					},
				},
				{
					Port:   "opa-metrics",
					Path:   "/metrics",
					Scheme: "https",
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-gateway-token",
							},
							Key: corev1.ServiceAccountTokenKey,
						},
					},
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gatewaySigningCABundleName("test-gateway"),
									},
									Key: caFile,
								},
							},
							ServerName: ptr.To("test-gateway-http.test.svc.cluster.local"),
						},
					},
				},
			},
		},
		{
			desc: "openshift-network",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 2,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "http",
				},
				{
					Port:   "opa-metrics",
					Path:   "/metrics",
					Scheme: "http",
				},
			},
		},
		{
			desc: "openshift-network with http encryption",
			opts: Options{
				Name:      "test",
				Namespace: "test",
				Image:     "test",
				Gates: configv1.FeatureGates{
					LokiStackGateway:           true,
					BuiltInCertManagement:      configv1.BuiltInCertManagement{Enabled: true},
					ServiceMonitors:            true,
					ServiceMonitorTLSEndpoints: true,
				},
				Stack: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
					Template: &lokiv1.LokiTemplateSpec{
						Gateway: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			total: 2,
			want: []monitoringv1.Endpoint{
				{
					Port:   gatewayInternalPortName,
					Path:   "/metrics",
					Scheme: "https",
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-gateway-token",
							},
							Key: corev1.ServiceAccountTokenKey,
						},
					},
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gatewaySigningCABundleName("test-gateway"),
									},
									Key: caFile,
								},
							},
							ServerName: ptr.To("test-gateway-http.test.svc.cluster.local"),
						},
					},
				},
				{
					Port:   "opa-metrics",
					Path:   "/metrics",
					Scheme: "https",
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-gateway-token",
							},
							Key: corev1.ServiceAccountTokenKey,
						},
					},
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gatewaySigningCABundleName("test-gateway"),
									},
									Key: caFile,
								},
							},
							ServerName: ptr.To("test-gateway-http.test.svc.cluster.local"),
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			sm := NewGatewayServiceMonitor(tc.opts)
			require.Len(t, sm.Spec.Endpoints, tc.total)

			for _, endpoint := range tc.want {
				require.Contains(t, sm.Spec.Endpoints, endpoint)
			}
		})
	}
}
