package main

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/pkg/sizing"
)

var Templaters = map[string]Templater{
	"jsonnet": JsonnetTemplater{},
}

type Templater interface {
	Template(sizing.ClusterResources) string
}

type JsonnetTemplater struct{}

func (t JsonnetTemplater) Template(cluster sizing.ClusterResources) string {
	var sb strings.Builder
	sb.WriteString(
		`local k = import 'ksonnet-util/kausal.libsonnet';
local deployment = k.apps.v1.deployment;
local statefulSet = k.apps.v1.statefulSet;
{
`)

	for _, c := range cluster.Components() {
		sb.WriteString(t.overrides(c))
	}

	sb.WriteString("}\n")

	return sb.String()
}

func (t JsonnetTemplater) overrides(component *sizing.ComponentDescription) string {
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
		return memcachedOverrides(component, "chunks")
	case sizing.QueryResultsCache:
		return memcachedOverrides(component, "frontend")
	case sizing.IndexCache:
		return memcachedOverrides(component, "index_queries")
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
    k.util.resourcesLimits('%s', '%s'),
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

func statefulsetOverrides(component *sizing.ComponentDescription, id string) string {
	return fmt.Sprintf(`
  %s_statefulset+:
    statefulSet.mixin.spec.withReplicas(%d),

  %s_container+::
    $.util.resourcesRequests('%s', '%s') +
    $.util.resourcesLimits('%s', '%s'),
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

func memcachedOverrides(component *sizing.ComponentDescription, id string) string {
	// Everything but replicas are actually managed by the memcached jsonnet library
	// and our memcached limits are derived from these, so avoid the complexity of trying to override it.
	return fmt.Sprintf(`
  memcached_%s+: {
    statefulSet+:
      statefulSet.mixin.spec.withReplicas(%d),
  },
`,
		id,
		component.Replicas,
	)
}
