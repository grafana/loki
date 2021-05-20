package manifests

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

const (
	gossipPort = 7946
	httpPort   = 3100
	grpcPort   = 9095

	// DefaultContainerImage declares the default fallback for loki image.
	DefaultContainerImage = "docker.io/grafana/loki:2.2.1"
)

const (
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
)

func commonAnnotations(h string) map[string]string {
	return map[string]string{
		"loki.openshift.io/config-hash": h,
	}
}

func commonLabels(stackName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "loki",
		"app.kubernetes.io/provider": "openshift",
		"loki.grafana.com/name":      stackName,
	}
}

// ComponentLabels is a list of all commonLabels including the loki.grafana.com/component:<component> label
func ComponentLabels(component, stackName string) labels.Set {
	return labels.Merge(commonLabels(stackName), map[string]string{
		"loki.grafana.com/component": component,
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
	return fmt.Sprintf("loki-compactor-%s", stackName)
}

// DistributorName is the name of the distibutor deployment
func DistributorName(stackName string) string {
	return fmt.Sprintf("loki-distributor-%s", stackName)
}

// IngesterName is the name of the compactor statefulset
func IngesterName(stackName string) string {
	return fmt.Sprintf("loki-ingester-%s", stackName)
}

// QuerierName is the name of the querier statefulset
func QuerierName(stackName string) string {
	return fmt.Sprintf("loki-querier-%s", stackName)
}

// QueryFrontendName is the name of the query-frontend statefulset
func QueryFrontendName(stackName string) string {
	return fmt.Sprintf("loki-query-frontend-%s", stackName)
}

func serviceNameQuerierHTTP(stackName string) string {
	return fmt.Sprintf("loki-querier-http-%s", stackName)
}

func serviceNameQuerierGRPC(stackName string) string {
	return fmt.Sprintf("loki-querier-grpc-%s", stackName)
}

func serviceNameIngesterGRPC(stackName string) string {
	return fmt.Sprintf("loki-ingester-grpc-%s", stackName)
}

func serviceNameIngesterHTTP(stackName string) string {
	return fmt.Sprintf("loki-ingester-http-%s", stackName)
}

func serviceNameDistributorGRPC(stackName string) string {
	return fmt.Sprintf("loki-distributor-grpc-%s", stackName)
}

func serviceNameDistributorHTTP(stackName string) string {
	return fmt.Sprintf("loki-distributor-http-%s", stackName)
}

func serviceNameCompactorGRPC(stackName string) string {
	return fmt.Sprintf("loki-compactor-grpc-%s", stackName)
}

func serviceNameCompactorHTTP(stackName string) string {
	return fmt.Sprintf("loki-compactor-http-%s", stackName)
}

func fqdn(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
}
