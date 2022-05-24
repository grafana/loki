package manifests

import (
	"fmt"

	"github.com/grafana/loki/operator/internal/manifests/openshift"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	gossipPort  = 7946
	httpPort    = 3100
	grpcPort    = 9095
	protocolTCP = "TCP"

	lokiHTTPPortName   = "metrics"
	lokiGRPCPortName   = "grpclb"
	lokiGossipPortName = "gossip-ring"

	lokiLivenessPath  = "/loki/api/v1/status/buildinfo"
	lokiReadinessPath = "/ready"

	gatewayContainerName    = "gateway"
	gatewayHTTPPort         = 8080
	gatewayInternalPort     = 8081
	gatewayHTTPPortName     = "public"
	gatewayInternalPortName = "metrics"

	// EnvRelatedImageLoki is the environment variable to fetch the Loki image pullspec.
	EnvRelatedImageLoki = "RELATED_IMAGE_LOKI"
	// EnvRelatedImageGateway is the environment variable to fetch the Gateway image pullspec.
	EnvRelatedImageGateway = "RELATED_IMAGE_GATEWAY"

	// DefaultContainerImage declares the default fallback for loki image.
	DefaultContainerImage = "docker.io/grafana/loki:2.5.0"

	// DefaultLokiStackGatewayImage declares the default image for lokiStack-gateway.
	DefaultLokiStackGatewayImage = "quay.io/observatorium/api:latest"

	// PrometheusCAFile declares the path for prometheus CA file for service monitors.
	PrometheusCAFile string = "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt"
	// BearerTokenFile declares the path for bearer token file for service monitors.
	BearerTokenFile string = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// labelJobComponent is a ServiceMonitor.Spec.JobLabel.
	labelJobComponent string = "loki.grafana.com/component"

	// LabelCompactorComponent is the label value for the compactor component
	LabelCompactorComponent string = "compactor"
	// LabelDistributorComponent is the label value for the distributor component
	LabelDistributorComponent string = "distributor"
	// LabelIngesterComponent is the label value for the ingester component
	LabelIngesterComponent string = "ingester"
	// LabelQuerierComponent is the label value for the querier component
	LabelQuerierComponent string = "querier"
	// LabelQueryFrontendComponent is the label value for the query frontend component
	LabelQueryFrontendComponent string = "query-frontend"
	// LabelIndexGatewayComponent is the label value for the lokiStack-index-gateway component
	LabelIndexGatewayComponent string = "index-gateway"
	// LabelRulerComponent is the label value for the lokiStack-ruler component
	LabelRulerComponent string = "ruler"
	// LabelGatewayComponent is the label value for the lokiStack-gateway component
	LabelGatewayComponent string = "lokistack-gateway"
)

var (
	defaultConfigMapMode = int32(420)
	volumeFileSystemMode = corev1.PersistentVolumeFilesystem
)

func commonAnnotations(h string) map[string]string {
	return map[string]string{
		"loki.grafana.com/config-hash": h,
	}
}

func commonLabels(stackName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "lokistack",
		"app.kubernetes.io/instance":   stackName,
		"app.kubernetes.io/managed-by": "lokistack-controller",
		"app.kubernetes.io/created-by": "lokistack-controller",
	}
}

func serviceAnnotations(serviceName string, enableSigningService bool) map[string]string {
	annotations := map[string]string{}
	if enableSigningService {
		annotations[openshift.ServingCertKey] = signingServiceSecretName(serviceName)
	}
	return annotations
}

// ComponentLabels is a list of all commonLabels including the app.kubernetes.io/component:<component> label
func ComponentLabels(component, stackName string) labels.Set {
	return labels.Merge(commonLabels(stackName), map[string]string{
		"app.kubernetes.io/component": component,
	})
}

// GossipLabels is the list of labels that should be assigned to components using the gossip ring
func GossipLabels() map[string]string {
	return map[string]string{
		"loki.grafana.com/gossip": "true",
	}
}

// CompactorName is the name of the compactor statefulset
func CompactorName(stackName string) string {
	return fmt.Sprintf("%s-compactor", stackName)
}

// DistributorName is the name of the distibutor deployment
func DistributorName(stackName string) string {
	return fmt.Sprintf("%s-distributor", stackName)
}

// IngesterName is the name of the compactor statefulset
func IngesterName(stackName string) string {
	return fmt.Sprintf("%s-ingester", stackName)
}

// QuerierName is the name of the querier deployment
func QuerierName(stackName string) string {
	return fmt.Sprintf("%s-querier", stackName)
}

// QueryFrontendName is the name of the query-frontend statefulset
func QueryFrontendName(stackName string) string {
	return fmt.Sprintf("%s-query-frontend", stackName)
}

// IndexGatewayName is the name of the index-gateway statefulset
func IndexGatewayName(stackName string) string {
	return fmt.Sprintf("%s-index-gateway", stackName)
}

// RulerName is the name of the ruler statefulset
func RulerName(stackName string) string {
	return fmt.Sprintf("%s-ruler", stackName)
}

// RulesConfigMapName is the name of the alerting rules configmap
func RulesConfigMapName(stackName string) string {
	return fmt.Sprintf("%s-rules", stackName)
}

// GatewayName is the name of the lokiStack-gateway statefulset
func GatewayName(stackName string) string {
	return fmt.Sprintf("%s-gateway", stackName)
}

// PrometheusRuleName is the name of the loki-prometheus-rule
func PrometheusRuleName(stackName string) string {
	return fmt.Sprintf("%s-prometheus-rule", stackName)
}

func serviceNameQuerierHTTP(stackName string) string {
	return fmt.Sprintf("%s-querier-http", stackName)
}

func serviceNameQuerierGRPC(stackName string) string {
	return fmt.Sprintf("%s-querier-grpc", stackName)
}

func serviceNameIngesterGRPC(stackName string) string {
	return fmt.Sprintf("%s-ingester-grpc", stackName)
}

func serviceNameIngesterHTTP(stackName string) string {
	return fmt.Sprintf("%s-ingester-http", stackName)
}

func serviceNameDistributorGRPC(stackName string) string {
	return fmt.Sprintf("%s-distributor-grpc", stackName)
}

func serviceNameDistributorHTTP(stackName string) string {
	return fmt.Sprintf("%s-distributor-http", stackName)
}

func serviceNameCompactorGRPC(stackName string) string {
	return fmt.Sprintf("%s-compactor-grpc", stackName)
}

func serviceNameCompactorHTTP(stackName string) string {
	return fmt.Sprintf("%s-compactor-http", stackName)
}

func serviceNameQueryFrontendGRPC(stackName string) string {
	return fmt.Sprintf("%s-query-frontend-grpc", stackName)
}

func serviceNameQueryFrontendHTTP(stackName string) string {
	return fmt.Sprintf("%s-query-frontend-http", stackName)
}

func serviceNameIndexGatewayHTTP(stackName string) string {
	return fmt.Sprintf("%s-index-gateway-http", stackName)
}

func serviceNameIndexGatewayGRPC(stackName string) string {
	return fmt.Sprintf("%s-index-gateway-grpc", stackName)
}

func serviceNameRulerHTTP(stackName string) string {
	return fmt.Sprintf("%s-ruler-http", stackName)
}

func serviceNameRulerGRPC(stackName string) string {
	return fmt.Sprintf("%s-ruler-grpc", stackName)
}

func serviceNameGatewayHTTP(stackName string) string {
	return fmt.Sprintf("%s-gateway-http", stackName)
}

func serviceMonitorName(componentName string) string {
	return fmt.Sprintf("%s-monitor", componentName)
}

func signingServiceSecretName(serviceName string) string {
	return fmt.Sprintf("%s-metrics", serviceName)
}

func fqdn(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
}

// serviceMonitorTLSConfig returns the TLS configuration for service monitors.
func serviceMonitorTLSConfig(serviceName, namespace string) monitoringv1.TLSConfig {
	return monitoringv1.TLSConfig{
		SafeTLSConfig: monitoringv1.SafeTLSConfig{
			// ServerName can be e.g. loki-distributor-http.openshift-logging.svc.cluster.local
			ServerName: fqdn(serviceName, namespace),
		},
		CAFile: PrometheusCAFile,
	}
}

// serviceMonitorEndpoint returns the lokistack endpoint for service monitors.
func serviceMonitorEndpoint(portName, serviceName, namespace string, enableTLS bool) monitoringv1.Endpoint {
	if enableTLS {
		tlsConfig := serviceMonitorTLSConfig(serviceName, namespace)
		return monitoringv1.Endpoint{
			Port:            portName,
			Path:            "/metrics",
			Scheme:          "https",
			BearerTokenFile: BearerTokenFile,
			TLSConfig:       &tlsConfig,
		}
	}

	return monitoringv1.Endpoint{
		Port:   portName,
		Path:   "/metrics",
		Scheme: "http",
	}
}

func lokiLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   lokiLivenessPath,
				Port:   intstr.FromInt(httpPort),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		TimeoutSeconds:   2,
		PeriodSeconds:    30,
		FailureThreshold: 10,
		SuccessThreshold: 1,
	}
}

func lokiReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   lokiReadinessPath,
				Port:   intstr.FromInt(httpPort),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		PeriodSeconds:       10,
		InitialDelaySeconds: 15,
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
}
