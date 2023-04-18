package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki/operator/internal/manifests/openshift"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

const (
	gossipPort       = 7946
	httpPort         = 3100
	internalHTTPPort = 3101
	grpcPort         = 9095
	protocolTCP      = "TCP"

	gossipInstanceAddrEnvVarName = "HASH_RING_INSTANCE_ADDR"

	lokiHTTPPortName         = "metrics"
	lokiInternalHTTPPortName = "healthchecks"
	lokiGRPCPortName         = "grpclb"
	lokiGossipPortName       = "gossip-ring"

	lokiLivenessPath  = "/loki/api/v1/status/buildinfo"
	lokiReadinessPath = "/ready"

	lokiFrontendContainerName = "loki-query-frontend"

	gatewayContainerName    = "gateway"
	gatewayHTTPPort         = 8080
	gatewayInternalPort     = 8081
	gatewayHTTPPortName     = "public"
	gatewayInternalPortName = "metrics"

	walVolumeName          = "wal"
	configVolumeName       = "config"
	rulesStorageVolumeName = "rules"
	storageVolumeName      = "storage"
	rulePartsSeparator     = "___"

	walDirectory          = "/tmp/wal"
	dataDirectory         = "/tmp/loki"
	rulesStorageDirectory = "/tmp/rules"

	rulerContainerName = "loki-ruler"

	// EnvRelatedImageLoki is the environment variable to fetch the Loki image pullspec.
	EnvRelatedImageLoki = "RELATED_IMAGE_LOKI"
	// EnvRelatedImageGateway is the environment variable to fetch the Gateway image pullspec.
	EnvRelatedImageGateway = "RELATED_IMAGE_GATEWAY"

	// DefaultContainerImage declares the default fallback for loki image.
	DefaultContainerImage = "docker.io/grafana/loki:2.8.0"

	// DefaultLokiStackGatewayImage declares the default image for lokiStack-gateway.
	DefaultLokiStackGatewayImage = "quay.io/observatorium/api:latest"

	// PrometheusCAFile declares the path for prometheus CA file for service monitors.
	PrometheusCAFile string = "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt"
	// BearerTokenFile declares the path for bearer token file for service monitors.
	BearerTokenFile string = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// labelJobComponent is a ServiceMonitor.Spec.JobLabel.
	labelJobComponent string = "loki.grafana.com/component"

	// AnnotationCertRotationRequiredAt stores the point in time the last cert rotation happened
	AnnotationCertRotationRequiredAt string = "loki.grafana.com/certRotationRequiredAt"
	// AnnotationLokiConfigHash stores the last SHA1 hash of the loki configuration
	AnnotationLokiConfigHash string = "loki.grafana.com/config-hash"

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

	// httpTLSDir is the path that is mounted from the secret for TLS
	httpTLSDir = "/var/run/tls/http"
	// grpcTLSDir is the path that is mounted from the secret for TLS
	grpcTLSDir = "/var/run/tls/grpc"
	// LokiStackCABundleDir is the path that is mounted from the configmap for TLS
	caBundleDir = "/var/run/ca"
	// caFile is the file name of the certificate authority file
	caFile = "service-ca.crt"

	kubernetesNodeOSLabel = "kubernetes.io/os"
	kubernetesNodeOSLinux = "linux"
)

var (
	defaultConfigMapMode = int32(420)
	volumeFileSystemMode = corev1.PersistentVolumeFilesystem
)

func commonAnnotations(configHash, rotationRequiredAt string) map[string]string {
	return map[string]string{
		AnnotationLokiConfigHash:         configHash,
		AnnotationCertRotationRequiredAt: rotationRequiredAt,
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
		annotations[openshift.ServingCertKey] = serviceName
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

// DistributorName is the name of the distributor deployment
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

// RulesConfigMapName is the name of the alerting/recording rules configmap
func RulesConfigMapName(stackName string) string {
	return fmt.Sprintf("%s-rules", stackName)
}

// RulesStorageVolumeName is the name of the rules volume
func RulesStorageVolumeName() string {
	return rulesStorageVolumeName
}

// GatewayName is the name of the lokiStack-gateway statefulset
func GatewayName(stackName string) string {
	return fmt.Sprintf("%s-gateway", stackName)
}

// PrometheusRuleName is the name of the loki-prometheus-rule
func PrometheusRuleName(stackName string) string {
	return fmt.Sprintf("%s-prometheus-rule", stackName)
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("%s-config", stackName)
}

func lokiServerGRPCTLSDir() string {
	return path.Join(grpcTLSDir, "server")
}

func lokiServerGRPCTLSCert() string {
	return path.Join(lokiServerGRPCTLSDir(), corev1.TLSCertKey)
}

func lokiServerGRPCTLSKey() string {
	return path.Join(lokiServerGRPCTLSDir(), corev1.TLSPrivateKeyKey)
}

func lokiServerHTTPTLSDir() string {
	return path.Join(httpTLSDir, "server")
}

func lokiServerHTTPTLSCert() string {
	return path.Join(lokiServerHTTPTLSDir(), corev1.TLSCertKey)
}

func lokiServerHTTPTLSKey() string {
	return path.Join(lokiServerHTTPTLSDir(), corev1.TLSPrivateKeyKey)
}

func gatewayServerHTTPTLSDir() string {
	return path.Join(httpTLSDir, "server")
}

func gatewayServerHTTPTLSCert() string {
	return path.Join(gatewayServerHTTPTLSDir(), corev1.TLSCertKey)
}

func gatewayServerHTTPTLSKey() string {
	return path.Join(gatewayServerHTTPTLSDir(), corev1.TLSPrivateKeyKey)
}

func gatewayUpstreamHTTPTLSDir() string {
	return path.Join(httpTLSDir, "upstream")
}

func gatewayUpstreamHTTPTLSCert() string {
	return path.Join(gatewayUpstreamHTTPTLSDir(), corev1.TLSCertKey)
}

func gatewayUpstreamHTTPTLSKey() string {
	return path.Join(gatewayUpstreamHTTPTLSDir(), corev1.TLSPrivateKeyKey)
}

func gatewayClientSecretName(stackName string) string {
	return fmt.Sprintf("%s-gateway-client-http", stackName)
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

func signingCABundleName(stackName string) string {
	return fmt.Sprintf("%s-ca-bundle", stackName)
}

func gatewaySigningCABundleName(gwName string) string {
	return fmt.Sprintf("%s-ca-bundle", gwName)
}

func gatewaySigningCADir() string {
	return path.Join(caBundleDir, "server")
}

func gatewaySigningCAPath() string {
	return path.Join(gatewaySigningCADir(), caFile)
}

func gatewayUpstreamCADir() string {
	return path.Join(caBundleDir, "upstream")
}

func gatewayUpstreamCAPath() string {
	return path.Join(gatewayUpstreamCADir(), caFile)
}

func gatewayTokenSecretName(gwName string) string {
	return fmt.Sprintf("%s-token", gwName)
}

func alertmanagerSigningCABundleName(rulerName string) string {
	return fmt.Sprintf("%s-ca-bundle", rulerName)
}

func alertmanagerUpstreamCADir() string {
	return path.Join(caBundleDir, "alertmanager")
}

func alertmanagerUpstreamCAPath() string {
	return path.Join(alertmanagerUpstreamCADir(), caFile)
}

func signingCAPath() string {
	return path.Join(caBundleDir, caFile)
}

func fqdn(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
}

// lokiServiceMonitorEndpoint returns the lokistack endpoint for service monitors.
func lokiServiceMonitorEndpoint(stackName, portName, serviceName, namespace string, enableTLS bool) monitoringv1.Endpoint {
	if enableTLS {
		tlsConfig := monitoringv1.TLSConfig{
			SafeTLSConfig: monitoringv1.SafeTLSConfig{
				CA: monitoringv1.SecretOrConfigMap{
					ConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: signingCABundleName(stackName),
						},
						Key: caFile,
					},
				},
				Cert: monitoringv1.SecretOrConfigMap{
					Secret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: serviceName,
						},
						Key: corev1.TLSCertKey,
					},
				},
				KeySecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: serviceName,
					},
					Key: corev1.TLSPrivateKeyKey,
				},
				// ServerName can be e.g. loki-distributor-http.openshift-logging.svc.cluster.local
				ServerName: fqdn(serviceName, namespace),
			},
		}

		return monitoringv1.Endpoint{
			Port:      portName,
			Path:      "/metrics",
			Scheme:    "https",
			TLSConfig: &tlsConfig,
		}
	}

	return monitoringv1.Endpoint{
		Port:   portName,
		Path:   "/metrics",
		Scheme: "http",
	}
}

// gatewayServiceMonitorEndpoint returns the lokistack endpoint for service monitors.
func gatewayServiceMonitorEndpoint(gatewayName, portName, serviceName, namespace string, enableTLS bool) monitoringv1.Endpoint {
	if enableTLS {
		tlsConfig := monitoringv1.TLSConfig{
			SafeTLSConfig: monitoringv1.SafeTLSConfig{
				CA: monitoringv1.SecretOrConfigMap{
					ConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: gatewaySigningCABundleName(gatewayName),
						},
						Key: caFile,
					},
				},
				// ServerName can be e.g. lokistack-dev-gateway-http.openshift-logging.svc.cluster.local
				ServerName: fqdn(serviceName, namespace),
			},
		}

		return monitoringv1.Endpoint{
			Port:   portName,
			Path:   "/metrics",
			Scheme: "https",
			BearerTokenSecret: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: gatewayTokenSecretName(gatewayName),
				},
				Key: corev1.ServiceAccountTokenKey,
			},
			TLSConfig: &tlsConfig,
		}
	}

	return monitoringv1.Endpoint{
		Port:   portName,
		Path:   "/metrics",
		Scheme: "http",
	}
}

func defaultAffinity(enableNodeAffinity bool) *corev1.Affinity {
	if !enableNodeAffinity {
		return nil
	}

	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      kubernetesNodeOSLabel,
								Operator: corev1.NodeSelectorOpIn,
								Values: []string{
									kubernetesNodeOSLinux,
								},
							},
						},
					},
				},
			},
		},
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

func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: pointer.Bool(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func podSecurityContext(withSeccompProfile bool) *corev1.PodSecurityContext {
	context := corev1.PodSecurityContext{
		RunAsNonRoot: pointer.Bool(true),
	}

	if withSeccompProfile {
		context.SeccompProfile = &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		}
	}

	return &context
}
