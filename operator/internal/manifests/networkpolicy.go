package manifests

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// BuildNetworkPolicies builds all NetworkPolicies required for a LokiStack deployment
func BuildNetworkPolicies(opts Options) []client.Object {
	rulerEnabled := opts.Stack.Rules != nil && opts.Stack.Rules.Enabled

	policies := []client.Object{
		buildDefaultDeny(opts),
		buildLokiAllow(opts),
		buildLokiAllowBucketEgress(opts),
	}

	if opts.Gates.LokiStackGateway {
		policies = append(policies,
			buildLokiAllowGatewayIngress(opts),
			buildGatewayAllow(opts),
		)
	}

	if opts.Gates.OpenShift.Enabled && opts.Gates.ServiceMonitors {
		policies = append(policies, buildLokiAllowMetrics(opts))
		if opts.Gates.LokiStackGateway {
			policies = append(policies, buildGatewayAllowMetrics(opts))
		}
	}

	if rulerEnabled {
		policies = append(policies, buildRulerAllowEgressToAM(opts))
	}

	if opts.Stack.Tenants.Mode == lokiv1.OpenshiftNetwork {
		policies = append(policies, buildLokiAllowQueryFrontend(opts))
	}

	return policies
}

var (
	selectorAllLokiComponents = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "app.kubernetes.io/name",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"lokistack"},
			},
			{
				Key:      "app.kubernetes.io/component",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"distributor", "ingester", "query-frontend", "querier", "ruler", "index-gateway", "compactor"},
			},
		},
	}

	selectorLokiGatewayPods = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/name":      "lokistack",
			"app.kubernetes.io/component": "lokistack-gateway",
		},
	}

	httpPolicyPort = networkingv1.NetworkPolicyPort{
		Protocol: ptr.To(corev1.ProtocolTCP),
		Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: httpPort},
	}
	internalHTTPPolicyPort = networkingv1.NetworkPolicyPort{
		Protocol: ptr.To(corev1.ProtocolTCP),
		Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: internalHTTPPort},
	}
	grpclbPolicyPort = networkingv1.NetworkPolicyPort{
		Protocol: ptr.To(corev1.ProtocolTCP),
		Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: grpcPort},
	}
	gossipPolicyPort = networkingv1.NetworkPolicyPort{
		Protocol: ptr.To(corev1.ProtocolTCP),
		Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gossipPort},
	}

	networkPolicyPeerPrometheusPods = networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "openshift-monitoring",
			},
		},
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/name": "prometheus",
			},
		},
	}

	egressToDNSK8s = networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			// Allow egress to any pod in kube-system namespace with DNS labels
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": "kube-system",
					},
				},
				PodSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "k8s-app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"kube-dns", "coredns"},
						},
					},
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{
				Protocol: ptr.To(corev1.ProtocolTCP),
				Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 53},
			},
			{
				Protocol: ptr.To(corev1.ProtocolUDP),
				Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 53},
			},
		},
	}
	egressToDNSOpenshift = networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			// For OpenShift compatibility
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": "openshift-dns",
					},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"dns.operator.openshift.io/daemonset-dns": "default",
					},
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{
				Protocol: ptr.To(corev1.ProtocolTCP),
				Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 5353},
			},
			{
				Protocol: ptr.To(corev1.ProtocolUDP),
				Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 5353},
			},
		},
	}
)

func lokiComponents(namespace string) networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		PodSelector: selectorAllLokiComponents,
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespace,
			},
		},
	}
}

// buildDefaultDeny default deny-all policy for the LokiStack components.
func buildDefaultDeny(opts Options) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-default-deny", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: commonLabels(opts.Name),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}

// buildLokiAllow NetworkPolicy to allow egress and ingress between the
// LokiStack components.
func buildLokiAllow(opts Options) *networkingv1.NetworkPolicy {
	egressToDNS := egressToDNSK8s
	if opts.Gates.OpenShift.Enabled {
		egressToDNS = egressToDNSOpenshift
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-loki-allow", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: *selectorAllLokiComponents,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{lokiComponents(opts.Namespace)},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
						internalHTTPPolicyPort,
						grpclbPolicyPort,
						gossipPolicyPort,
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				egressToDNS,
				// Egress to common Loki ports for gRPC load balancing & gossip ring
				{
					To: []networkingv1.NetworkPolicyPeer{lokiComponents(opts.Namespace)},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
						internalHTTPPolicyPort,
						grpclbPolicyPort,
						gossipPolicyPort,
					},
				},
			},
		},
	}
}

// buildLokiAllowMetrics NetworkPolicy to allow ingress from Prometheus to the
// LokiStack components.
func buildLokiAllowMetrics(opts Options) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-loki-allow-metrics", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: *selectorAllLokiComponents,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						networkPolicyPeerPrometheusPods,
					},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
					},
				},
			},
		},
	}
}

// buildLokiAllowGatewayIngress NetworkPolicy to allow ingress from the
// gateway to the necessary components.
func buildLokiAllowGatewayIngress(opts Options) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-loki-allow-gateway-ingress", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app.kubernetes.io/name",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"lokistack"},
					},
					{
						Key:      "app.kubernetes.io/component",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"distributor", "query-frontend", "ruler"},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": opts.Namespace,
								},
							},
							PodSelector: selectorLokiGatewayPods,
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
					},
				},
			},
		},
	}
}

// buildLokiAllowBucketEgress NetworkPolicy to allow egress traffic from
// components that need to access object storage to object storage
func buildLokiAllowBucketEgress(opts Options) *networkingv1.NetworkPolicy {
	objstorePort := int32(443) // Default HTTPS port
	if port := getEndpointPort(opts.ObjectStorage); port != 0 {
		objstorePort = port
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-loki-allow-bucket-egress", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app.kubernetes.io/name",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"lokistack"},
					},
					{
						Key:      "app.kubernetes.io/component",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"ingester", "querier", "index-gateway", "compactor", "ruler"},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// Allow egress to object storage
				{
					To: []networkingv1.NetworkPolicyPeer{},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: objstorePort},
						},
					},
				},
			},
		},
	}
}

// buildGatewayAllow NetworkPolicy to allow ingress and egress traffic from
// gateway pods
func buildGatewayAllow(opts Options) *networkingv1.NetworkPolicy {
	egressToDNS := egressToDNSK8s
	if opts.Gates.OpenShift.Enabled {
		egressToDNS = egressToDNSOpenshift
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-gateway-allow", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: *selectorLokiGatewayPods,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
				networkingv1.PolicyTypeIngress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				egressToDNS,
				// Allow egress to query-frontend for queries
				// Allow egress to distributor for pushing logs
				// Allow egress to ruler for getting rules
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": opts.Namespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app.kubernetes.io/name",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"lokistack"},
									},
									{
										Key:      "app.kubernetes.io/component",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"distributor", "query-frontend", "ruler"},
									},
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
					},
				},
				// Allow egress to the API server for token requests
				{
					To: []networkingv1.NetworkPolicyPeer{},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 6443},
						},
					},
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// Allow ingress to gateway from both in-cluster & route
				{
					From: []networkingv1.NetworkPolicyPeer{},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gatewayHTTPPort},
						},
					},
				},
			},
		},
	}
}

func buildGatewayAllowMetrics(opts Options) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-gateway-allow-metrics", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: *selectorLokiGatewayPods,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						networkPolicyPeerPrometheusPods,
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gatewayInternalPort},
						},
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: gatewayInternalOPAPort},
						},
					},
				},
			},
		},
	}
}

func buildRulerAllowEgressToAM(opts Options) *networkingv1.NetworkPolicy {
	parseAlertManagerPorts := func(endpoints []string) []int32 {
		portSet := make(map[int32]bool)

		for _, endpoint := range endpoints {
			port := int32(9093) // default port

			// Parse the URL to extract port if specified
			if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
				if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
					port = int32(p)
				}
			}

			portSet[port] = true
		}

		// Convert map keys to slice
		ports := make([]int32, 0, len(portSet))
		for port := range portSet {
			ports = append(ports, port)
		}

		return ports
	}
	var egressRules []networkingv1.NetworkPolicyEgressRule

	// Allow egress to cluster monitoring Alertmanager if enabled
	if opts.OpenShiftOptions.BuildOpts.AlertManagerEnabled {
		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "openshift-monitoring",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name": "alertmanager",
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Protocol: ptr.To(corev1.ProtocolTCP),
					Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 9095},
				},
			},
		})
	}

	// Allow egress to user workload monitoring Alertmanager if enabled
	if opts.OpenShiftOptions.BuildOpts.UserWorkloadAlertManagerEnabled {
		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "openshift-user-workload-monitoring",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name": "alertmanager",
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Protocol: ptr.To(corev1.ProtocolTCP),
					Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 9095},
				},
			},
		})
	}

	// Allow egress to any Alertmanager if enabled
	if opts.Ruler.Spec != nil && opts.Ruler.Spec.AlertManagerSpec != nil {
		ports := parseAlertManagerPorts(opts.Ruler.Spec.AlertManagerSpec.Endpoints)

		var networkPorts []networkingv1.NetworkPolicyPort
		for _, port := range ports {
			networkPorts = append(networkPorts, networkingv1.NetworkPolicyPort{
				Protocol: ptr.To(corev1.ProtocolTCP),
				Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: port},
			})
		}

		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			To:    []networkingv1.NetworkPolicyPeer{},
			Ports: networkPorts,
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ruler-allow-alert-egress", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "lokistack",
					"app.kubernetes.io/component": "ruler",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: egressRules,
		},
	}
}

func buildLokiAllowQueryFrontend(opts Options) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-loki-allow-query-frontend", opts.Name),
			Namespace: opts.Namespace,
			Labels:    commonLabels(opts.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "lokistack",
					"app.kubernetes.io/component": "query-frontend",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{},
					Ports: []networkingv1.NetworkPolicyPort{
						httpPolicyPort,
					},
				},
			},
		},
	}
}

func getEndpointPort(storageOpts storage.Options) int32 {
	extractPort := func(endpoint string) int32 {
		if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
			if u, err := url.Parse(endpoint); err == nil && u.Port() != "" {
				if port, err := strconv.Atoi(u.Port()); err == nil {
					return int32(port)
				}
			}
			return 0
		}

		if strings.Contains(endpoint, ":") {
			if epParts := strings.Split(endpoint, ":"); len(epParts) >= 2 {
				portStr := epParts[len(epParts)-1] // last position should be the port
				if idx := strings.Index(portStr, "/"); idx != -1 {
					portStr = portStr[:idx] // remove the path if present
				}
				if port, err := strconv.Atoi(portStr); err == nil {
					return int32(port)
				}
			}
		}

		return 0
	}
	// Many self-hosted object storage solutions use S3 API endpoints
	// so we have to check for a port
	if storageOpts.S3 != nil && storageOpts.S3.Endpoint != "" {
		return extractPort(storageOpts.S3.Endpoint)
	}

	// AlibabaCloud Endpoint might includes ports
	if storageOpts.AlibabaCloud != nil && storageOpts.AlibabaCloud.Endpoint != "" {
		return extractPort(storageOpts.AlibabaCloud.Endpoint)
	}

	// Swift AuthURL might includes ports
	if storageOpts.Swift != nil && storageOpts.Swift.AuthURL != "" {
		return extractPort(storageOpts.Swift.AuthURL)
	}

	return 0
}
