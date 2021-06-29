package main

import (
	"fmt"
	"math"

	"github.com/grafana/loki/pkg/sizing"
	"github.com/grafana/loki/pkg/validation"
)

func main() {}

// Write Path: distributor, ingester
// Read Path: Query frontend, querier, memcached, memcached-index-queries, memcached-frontend,
// Cluster svcs: ruler, Compactor, (table manager - ignore and assume we're using the new retention), index-gw
func Sizes(limits validation.Limits) {
	// size
}

/*
   The following uses calculations:

   # replicas by throughput
   sum(rate(loki_distributor_bytes_received_total[5m])) by (cluster, namespace)
   /
   count by (cluster, namespace) (kube_pod_container_info{image=~".*loki.*", container="distributor"}) / 1e6

   # memory requests
   avg by (cluster, namespace) (kube_pod_container_resource_requests{container="distributor", namespace=~"$namespace", resource="memory"})
   / 1e9

   # memory limits
   avg by (cluster, namespace) (kube_pod_container_resource_limits{container="distributor", namespace=~"$namespace", resource="memory"})
   / 1e9

   # cpu requests
   avg by (cluster, namespace) (kube_pod_container_resource_requests{container="distributor", namespace=~"$namespace", resource="cpu"})

   # cpu limits
   avg by (cluster, namespace) (kube_pod_container_resource_requests{container="distributor", namespace=~"$namespace", resource="cpu"})
*/

func distributorSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 5.e6
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// maintain minimum two for HA
	if n < 2 {
		n = 2
	}

	component.Name = sizing.Distributor
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("500KB")
	_ = component.Resources.MemoryLimits.Set("1GB")
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(0.5)
	return
}

func ingesterSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 2.8
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	// maintain minimum 3 for replication
	if n < 3 {
		n = 3
	}

	component.Name = sizing.Ingester
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("7GB")
	_ = component.Resources.MemoryLimits.Set("14GB")
	component.Resources.CPURequests.SetCores(1)
	component.Resources.CPULimits.SetCores(2)
	component.Resources.DiskGB = 150
	return
}

func queryFrontendSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	// run two for HA
	n := 2

	component.Name = sizing.QueryFrontend
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("5GB")
	_ = component.Resources.MemoryLimits.Set("10GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(3)
	return
}

func querierSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 2.5
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	// require a minimum number of queriers to be able to reasonably parallelize workloads
	if n < 8 {
		n = 8
	}

	component.Name = sizing.Querier
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("6GB")
	_ = component.Resources.MemoryLimits.Set("16GB")
	component.Resources.CPURequests.SetCores(4)
	component.Resources.CPULimits.SetCores(7)
	return
}

func memcachedChunksSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 2.
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	component.Name = sizing.ChunksCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("5GB")
	_ = component.Resources.MemoryLimits.Set("6GB")
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return
}

func memcachedFrontendSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 23.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	component.Name = sizing.QueryResultsCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set(fmt.Sprintf("%dMB", int(1024*1.5)))
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return
}

func memcachedIndexQueriesSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 14.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	component.Name = sizing.IndexCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set(fmt.Sprintf("%dMB", int(1024*1.5)))
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return
}

func rulerIndexQueriesSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	// use a constant two rulers for now
	n := 2

	component.Name = sizing.Ruler
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("6GB")
	_ = component.Resources.MemoryLimits.Set("16GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(7)
	return
}

func compactorSizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	n := 1

	component.Name = sizing.Compactor
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set("2GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(4)
	component.Resources.DiskGB = 100
	return
}

func indexGatewaySizing(ingestionRateMB float64) (component sizing.ComponentDescription) {
	MBSecondPerInstance := 45.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	component.Name = sizing.IndexGateway
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set("3GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(2)
	component.Resources.DiskGB = 400
	return
}

func replicas(perInstance, ingestionRate float64) int {
	return int(math.Ceil(ingestionRate / perInstance))
}
