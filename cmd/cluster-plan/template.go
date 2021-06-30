package main

import (
	"fmt"

	"github.com/grafana/loki/pkg/sizing"
)

var Templaters = map[string]Templater{
	"jsonnet": JsonnetTemplater{},
}

type Templater interface {
	Template(sizing.ClusterResources) string
}

type JsonnetTemplater struct{}

func (_ JsonnetTemplater) Template(cluster sizing.ClusterResources) string {
	return ""
}

func (_ JsonnetTemplater) overrides(component *sizing.ComponentDescription) string {
	switch component.Name {
	case sizing.Distributor:
		return deploymentOverrides(component, "distributor")
	case sizing.Ingester:
		return statefulsetOverrides(component, "ingester")
	case sizing.Querier:
		return deploymentOverrides(component, "querier")
	case sizing.QueryFrontend:
		return deploymentOverrides(component, "query_frontend")
	case sizing.Ruler:
		return deploymentOverrides(component, "ruler")
	case sizing.Compactor:
		return statefulsetOverrides(component, "compactor")
	case sizing.ChunksCache:
		return memcachedOverrides(component, "memcached_chunks")
	case sizing.QueryResultsCache:
		return memcachedOverrides(component, "memcached_frontend")
	case sizing.IndexCache:
		return memcachedOverrides(component, "memcached_index_queries")
	case sizing.IndexGateway:
		return statefulsetOverrides(component, "index_gateway")
	default:
		return ""
	}
}

func deploymentOverrides(component *sizing.ComponentDescription, id string) string {
	return fmt.Sprintf(`
  %s_deployment+:
    deployment.mixin.spec.withReplicas(%d),

  %s_container+::
    k.util.resourcesRequests('%s', '%s') +
    k.util.resourcesLimits('%s', '16Gi'),
`,
		id,
		component.Replicas,
		id,
		component.Resources.CPURequests.Kubernetes(),
		sizing.ReadableBytes(component.Resources.MemoryRequests).Kubernetes(),
		component.Resources.CPULimits.Kubernetes(),
		sizing.ReadableBytes(component.Resources.MemoryLimits).Kubernetes(),
	)
}

func statefulsetOverrides(component *sizing.ComponentDescription) string {}

func memcachedOverrides(component *sizing.ComponentDescription) string {}

// local k = import 'ksonnet-util/kausal.libsonnet',
// local deployment = k.apps.v1.deployment,
// local statefulSet = k.apps.v1.statefulSet,
