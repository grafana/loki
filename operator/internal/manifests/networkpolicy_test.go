package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
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
				},
			},
			expectedPolicyCount: 7,
			expectedPolicyNames: []string{
				"test-default-deny",
				"test-loki-allow",
				"test-loki-allow-bucket-egress",
				"test-loki-allow-gateway-ingress",
				"test-gateway-allow",
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
		name                           string
		opts                           Options
		expectOpenShiftPromRestriction bool
	}{
		{
			name: "static mode - no prometheus restrictions",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
			expectOpenShiftPromRestriction: false,
		},
		{
			name: "openshift logging mode - prometheus restrictions",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
			},
			expectOpenShiftPromRestriction: true,
		},
		{
			name: "openshift network mode - prometheus restrictions",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
			},
			expectOpenShiftPromRestriction: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := buildLokiAllow(tt.opts)

			require.NotNil(t, policy)
			require.Equal(t, "test-loki-allow", policy.Name)
			require.Equal(t, "test-ns", policy.Namespace)

			require.Len(t, policy.Spec.Egress, 3)  // DNS K8s, DNS OpenShift, Loki components
			require.Len(t, policy.Spec.Ingress, 2) // Loki components, Prometheus

			firstRule := policy.Spec.Ingress[0]
			require.Len(t, firstRule.From, 1)
			require.NotNil(t, firstRule.From[0].PodSelector)

			secondRule := policy.Spec.Ingress[1]
			require.Len(t, secondRule.From, 1)

			if tt.expectOpenShiftPromRestriction {
				promPeer := secondRule.From[0]
				require.NotNil(t, promPeer.NamespaceSelector)
				require.NotNil(t, promPeer.PodSelector)
				require.Equal(t, "openshift-monitoring", promPeer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
				require.Equal(t, "prometheus", promPeer.PodSelector.MatchLabels["app.kubernetes.io/name"])
			} else {
				promPeer := secondRule.From[0]
				require.Nil(t, promPeer.NamespaceSelector)
				require.Nil(t, promPeer.PodSelector)
			}
		})
	}
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
	opts := Options{
		Name:      "test",
		Namespace: "test-ns",
	}

	policy := buildLokiAllowBucketEgress(opts)

	require.NotNil(t, policy)
	require.Equal(t, "test-loki-allow-bucket-egress", policy.Name)
	require.Equal(t, "test-ns", policy.Namespace)

	require.Len(t, policy.Spec.PodSelector.MatchExpressions, 2)
	componentExpr := policy.Spec.PodSelector.MatchExpressions[1]
	require.Equal(t, "app.kubernetes.io/component", componentExpr.Key)
	require.ElementsMatch(t, []string{"ingester", "querier", "index-gateway", "compactor", "ruler"}, componentExpr.Values)

	require.Len(t, policy.Spec.Egress, 1) // Egress to object storage
	require.Empty(t, policy.Spec.Ingress)

	egressRule := policy.Spec.Egress[0]
	require.Empty(t, egressRule.To)

	require.Len(t, egressRule.Ports, 3)
	expectedPorts := []int32{443, 9000, 8080}
	actualPorts := make([]int32, len(egressRule.Ports))
	for i, port := range egressRule.Ports {
		actualPorts[i] = port.Port.IntVal
	}
	require.ElementsMatch(t, expectedPorts, actualPorts)
}

func TestBuildGatewayAllow(t *testing.T) {
	tests := []struct {
		name                           string
		opts                           Options
		expectOpenShiftPromRestriction bool
	}{
		{
			name: "static mode - no prometheus restrictions",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
			expectOpenShiftPromRestriction: false,
		},
		{
			name: "openshift logging mode - prometheus restrictions",
			opts: Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
			},
			expectOpenShiftPromRestriction: true,
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

			require.Len(t, policy.Spec.Egress, 4)  // DNS K8s, DNS OpenShift, Loki components, API server
			require.Len(t, policy.Spec.Ingress, 2) // Prometheus metrics, external gateway access

			promRule := policy.Spec.Ingress[0]
			require.Len(t, promRule.From, 1)

			if tt.expectOpenShiftPromRestriction {
				promPeer := promRule.From[0]
				require.NotNil(t, promPeer.NamespaceSelector)
				require.NotNil(t, promPeer.PodSelector)
				require.Equal(t, "openshift-monitoring", promPeer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
				require.Equal(t, "prometheus", promPeer.PodSelector.MatchLabels["app.kubernetes.io/name"])
			} else {
				promPeer := promRule.From[0]
				require.Nil(t, promPeer.NamespaceSelector)
				require.Nil(t, promPeer.PodSelector)
			}

			externalRule := policy.Spec.Ingress[1]
			require.Empty(t, externalRule.From)
		})
	}
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
