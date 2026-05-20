package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestBuildNetworkPolicies(t *testing.T) {
	tests := []struct {
		name                string
		opts                Options
		expectedPolicyCount int
		expectedPolicyNames []string
	}{
		{
			name: "basic lokistack without gateway",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: false,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway: false,
				},
			},
			expectedPolicyCount: 3,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
			},
		},
		{
			name: "lokistack with gateway enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: false,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
				},
			},
			expectedPolicyCount: 5,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
				"test-loki-allow-gateway-ingress",
				"test-gateway-allow",
			},
		},
		{
			name: "lokistack with ruler enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway: false,
				},
			},
			expectedPolicyCount: 4,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
				"test-ruler-allow-alert-egress",
			},
		},
		{
			name: "openshift network mode",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: false,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway: false,
				},
			},
			expectedPolicyCount: 4,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
				"test-loki-allow-query-frontend",
			},
		},
		{
			name: "full featured lokistack",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Gates: configv1.FeatureGates{
					LokiStackGateway: true,
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
					ServiceMonitors: true,
				},
			},
			expectedPolicyCount: 9,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
				"test-loki-allow-gateway-ingress",
				"test-gateway-allow",
				"test-loki-allow-metrics",
				"test-gateway-allow-metrics",
				"test-ruler-allow-alert-egress",
				"test-loki-allow-query-frontend",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policies := BuildNetworkPolicies(tt.opts)

			require.Len(t, policies, tt.expectedPolicyCount)

			policyNames := make([]string, len(policies))
			for i, policy := range policies {
				np := policy.(*networkingv1.NetworkPolicy)
				policyNames[i] = np.Name
			}

			require.ElementsMatch(t, tt.expectedPolicyNames, policyNames)
		})
	}
}

func TestBuildDefaultDeny(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
	}

	policy := buildDefaultDeny(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-default-deny", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.Equal(t, policy.Spec.PodSelector.MatchLabels, map[string]string{
		"app.kubernetes.io/name":       "lokistack",
		"app.kubernetes.io/instance":   "test",
		"app.kubernetes.io/managed-by": "lokistack-controller",
		"app.kubernetes.io/created-by": "lokistack-controller",
	})
	require.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	require.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	require.Empty(t, policy.Spec.Ingress)
	require.Empty(t, policy.Spec.Egress)
}

func TestBuildLokiAllow(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "k8s mode - k8s dns",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
		},
		{
			name: "openshift mode - openshift dns",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := buildLokiAllow(tt.opts)

			require.NotNil(t, policy)
			require.Equal(t, "test-loki-allow", policy.Name)
			require.Equal(t, "test-ns", policy.Namespace)

			require.Len(t, policy.Spec.Egress, 2)  // DNS K8s, DNS OpenShift, Loki components
			require.Len(t, policy.Spec.Ingress, 1) // Loki components

			ingressRule := policy.Spec.Ingress[0]
			require.Len(t, ingressRule.From, 1)
			require.NotNil(t, ingressRule.From[0].PodSelector)
			if tt.opts.Gates.OpenShift.Enabled {
				require.Equal(t, policy.Spec.Egress[0], egressToDNSOpenshift)
			} else {
				require.Equal(t, policy.Spec.Egress[0], egressToDNSK8s)
			}
		})
	}
}

func TestBuildLokiAllowMetrics(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
	}

	policy := buildLokiAllowMetrics(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-loki-allow-metrics", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.Len(t, policy.Spec.Egress, 0)
	require.Len(t, policy.Spec.Ingress, 1)

	ingressRule := policy.Spec.Ingress[0]
	require.Equal(t, networkPolicyPeerPrometheusPods, ingressRule.From[0])
	require.Len(t, ingressRule.Ports, 1)
	require.Equal(t, httpPolicyPort, ingressRule.Ports[0])
}

func TestBuildLokiAllowGatewayIngress(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
	}

	policy := buildLokiAllowGatewayIngress(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-loki-allow-gateway-ingress", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.Len(t, policy.Spec.PodSelector.MatchExpressions, 2)
	componentExpr := policy.Spec.PodSelector.MatchExpressions[1]
	require.Equal(t, "app.kubernetes.io/component", componentExpr.Key)
	require.ElementsMatch(t, []string{"distributor", "query-frontend", "ruler"}, componentExpr.Values)

	require.Empty(t, policy.Spec.Egress)
	require.Len(t, policy.Spec.Ingress, 1) // Ingress from gateway

	ingressRule := policy.Spec.Ingress[0]
	require.Len(t, ingressRule.From, 1)
	fromPeer := ingressRule.From[0]
	require.NotNil(t, fromPeer.NamespaceSelector)
	require.Equal(t, "test-ns", fromPeer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
	require.NotNil(t, fromPeer.PodSelector)
	require.Equal(t, "lokistack-gateway", fromPeer.PodSelector.MatchLabels["app.kubernetes.io/component"])
}

func TestBuildLokiAllowBucketEgress(t *testing.T) {
	tests := []struct {
		name          string
		opts          Options
		expectedPorts []int32
	}{
		{
			name: "AWS S3 endpoint without port (defaults to 443)",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretS3,
					S3: &storage.S3StorageConfig{
						Endpoint: "https://s3.amazonaws.com",
					},
				},
			},
			expectedPorts: []int32{443},
		},
		{
			name: "MinIO k8s service endpoint with custom port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretS3,
					S3: &storage.S3StorageConfig{
						Endpoint: "http://minio.test.svc.cluster.local:9000",
					},
				},
			},
			expectedPorts: []int32{9000},
		},
		{
			name: "MinIO simple hostname with port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretS3,
					S3: &storage.S3StorageConfig{
						Endpoint: "minio.test.svc.cluster.local:8080",
					},
				},
			},
			expectedPorts: []int32{8080},
		},
		{
			name: "Swift endpoint with default SSL port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretSwift,
					Swift: &storage.SwiftStorageConfig{
						AuthURL: "http://keystone.openstack.svc.cluster.local:5000/v3",
					},
				},
			},
			expectedPorts: []int32{5000, 443},
		},
		{
			name: "Swift endpoint with OpenStack OpenShift default SSL port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretSwift,
					Swift: &storage.SwiftStorageConfig{
						AuthURL: "http://keystone.openstack.svc.cluster.local:5000/v3",
					},
				},
			},
			expectedPorts: []int32{5000, 13808},
		},
		{
			name: "AlibabaCloud endpoint with custom port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretAlibabaCloud,
					AlibabaCloud: &storage.AlibabaCloudStorageConfig{
						Endpoint: "http://oss-emulator.default.svc.cluster.local:8080",
					},
				},
			},
			expectedPorts: []int32{8080},
		},
		{
			name: "AlibabaCloud endpoint without port (defaults to 443)",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretAlibabaCloud,
					AlibabaCloud: &storage.AlibabaCloudStorageConfig{
						Endpoint: "https://oss-cn-hangzhou.aliyuncs.com",
					},
				},
			},
			expectedPorts: []int32{443},
		},
		{
			name: "HTTPS proxy endpoint with custom port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				ObjectStorage: storage.Options{
					SharedStore: lokiv1.ObjectStorageSecretS3,
					S3: &storage.S3StorageConfig{
						Endpoint: "https://s3.amazonaws.com",
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Proxy: &lokiv1.ClusterProxy{
						HTTPProxy:  "http://proxy.example.com:8080",
						HTTPSProxy: "http://proxy.example.com:6443",
					},
				},
			},
			expectedPorts: []int32{443, 8080, 6443},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := buildLokiAllowBucketEgress(tc.opts)

			require.NotNil(t, policy)
			require.Equal(t, "test-loki-allow-bucket-egress", policy.Name)
			require.Equal(t, "test-ns", policy.Namespace)

			// Verify pod selector
			require.Len(t, policy.Spec.PodSelector.MatchExpressions, 2)
			componentExpr := policy.Spec.PodSelector.MatchExpressions[1]
			require.Equal(t, "app.kubernetes.io/component", componentExpr.Key)
			require.ElementsMatch(t, []string{"ingester", "querier", "index-gateway", "compactor", "ruler"}, componentExpr.Values)

			// Verify egress rules
			require.Len(t, policy.Spec.Egress, 1, "Should have exactly one egress rule")
			require.Empty(t, policy.Spec.Ingress, "Should have no ingress rules")

			egressRule := policy.Spec.Egress[0]
			require.Empty(t, egressRule.To, "Egress should allow to any destination")

			// Verify the port
			require.Len(t, egressRule.Ports, len(tc.expectedPorts), "Ports array should have the expected length")
			for i, port := range egressRule.Ports {
				require.Equal(t, tc.expectedPorts[i], port.Port.IntVal, "Port should match expected value")
			}
		})
	}
}

func TestBuildGatewayAllow(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "k8s mode - k8s dns",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
		},
		{
			name: "openshift mode - openshift dns",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Gates: configv1.FeatureGates{
					OpenShift: configv1.OpenShiftFeatureGates{
						Enabled: true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := buildGatewayAllow(tt.opts)

			require.NotNil(t, policy)
			require.Equal(t, "test-gateway-allow", policy.Name)
			require.Equal(t, "test-ns", policy.Namespace)

			require.Equal(t, "lokistack", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])
			require.Equal(t, "lokistack-gateway", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/component"])

			require.Len(t, policy.Spec.Egress, 3)  // DNS K8s, DNS OpenShift, Loki components, API server
			require.Len(t, policy.Spec.Ingress, 1) // external gateway access

			if tt.opts.Gates.OpenShift.Enabled {
				require.Equal(t, policy.Spec.Egress[0], egressToDNSOpenshift)
			} else {
				require.Equal(t, policy.Spec.Egress[0], egressToDNSK8s)
			}

			externalRule := policy.Spec.Ingress[0]
			require.Empty(t, externalRule.From)
		})
	}
}

func TestBuildGatewayAllowMetrics(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
	}

	policy := buildGatewayAllowMetrics(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-gateway-allow-metrics", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.NotNil(t, policy.Spec.PodSelector)
	require.Equal(t, "lokistack", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])
	require.Equal(t, "lokistack-gateway", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/component"])

	require.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	require.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	require.Len(t, policy.Spec.Egress, 0)
	require.Len(t, policy.Spec.Ingress, 1)

	ingressRule := policy.Spec.Ingress[0]
	require.Equal(t, networkPolicyPeerPrometheusPods, ingressRule.From[0])
	require.Len(t, ingressRule.Ports, 2)
	require.Equal(t, ingressRule.Ports, []networkingv1.NetworkPolicyPort{
		{
			Protocol: ptr.To(corev1.ProtocolTCP),
			Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gatewayInternalPort},
		},
		{
			Protocol: ptr.To(corev1.ProtocolTCP),
			Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gatewayInternalOPAPort},
		},
	})
}

func TestBuildRulerAllowEgressToAM(t *testing.T) {
	tests := []struct {
		name                string
		opts                Options
		expectedEgressRules int
		expectedPorts       []int32
	}{
		{
			name: "no alertmanager enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: false,
					},
				},
				Ruler: Ruler{
					Spec: nil,
				},
			},
			expectedEgressRules: 0,
			expectedPorts:       []int32{},
		},
		{
			name: "cluster alertmanager enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             true,
						UserWorkloadAlertManagerEnabled: false,
					},
				},
				Ruler: Ruler{
					Spec: nil,
				},
			},
			expectedEgressRules: 1,
			expectedPorts:       []int32{9095},
		},
		{
			name: "user workload alertmanager enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
				Ruler: Ruler{
					Spec: nil,
				},
			},
			expectedEgressRules: 1,
			expectedPorts:       []int32{9095},
		},
		{
			name: "both openshift alertmanagers enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             true,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
				Ruler: Ruler{
					Spec: nil,
				},
			},
			expectedEgressRules: 2,
			expectedPorts:       []int32{9095, 9095},
		},
		{
			name: "custom alertmanager enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: false,
					},
				},
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							Endpoints: []string{"http://alertmanager.example.com:9093"},
						},
					},
				},
			},
			expectedEgressRules: 1,
			expectedPorts:       []int32{9093},
		},
		{
			name: "custom alertmanager with custom port",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: false,
					},
				},
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							Endpoints: []string{"http://alertmanager.example.com:8080"},
						},
					},
				},
			},
			expectedEgressRules: 1,
			expectedPorts:       []int32{8080},
		},
		{
			name: "custom alertmanager without port defaults to 9093",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: false,
					},
				},
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							Endpoints: []string{"http://alertmanager.example.com"},
						},
					},
				},
			},
			expectedEgressRules: 1,
			expectedPorts:       []int32{9093},
		},
		{
			name: "all alertmanagers enabled",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             true,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							Endpoints: []string{"http://alertmanager.example.com:9093"},
						},
					},
				},
			},
			expectedEgressRules: 3,
			expectedPorts:       []int32{9095, 9095, 9093},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := buildRulerAllowEgressToAM(tt.opts)

			require.NotNil(t, policy)
			require.Equal(t, "test-ruler-allow-alert-egress", policy.Name)
			require.Equal(t, "test-ns", policy.Namespace)

			// Check pod selector targets ruler
			require.Equal(t, "lokistack", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])
			require.Equal(t, "ruler", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/component"])

			require.Empty(t, policy.Spec.Ingress)
			require.Len(t, policy.Spec.Egress, tt.expectedEgressRules)

			// Check ports
			actualPorts := make([]int32, 0)
			for _, rule := range policy.Spec.Egress {
				for _, port := range rule.Ports {
					actualPorts = append(actualPorts, port.Port.IntVal)
				}
			}
			require.ElementsMatch(t, tt.expectedPorts, actualPorts)
		})
	}
}

func TestBuildLokiAllowQueryFrontend(t *testing.T) {
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
		Stack: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftNetwork,
			},
		},
	}

	policy := buildLokiAllowQueryFrontend(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-loki-allow-query-frontend", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.Equal(t, "lokistack", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])
	require.Equal(t, "query-frontend", policy.Spec.PodSelector.MatchLabels["app.kubernetes.io/component"])

	require.Empty(t, policy.Spec.Egress)
	require.Len(t, policy.Spec.Ingress, 1)

	ingressRule := policy.Spec.Ingress[0]
	require.Empty(t, ingressRule.From)
	require.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
}
