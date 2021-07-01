package sizing

import (
	"fmt"
	"math"
)

func SizeCluster(bytesThroughput int) (cluster ClusterResources) {
	ingestionRateMB := float64(bytesThroughput) / (1 << 20)

	cluster.Distributor = distributorSizing(ingestionRateMB)
	cluster.Ingester = ingesterSizing(ingestionRateMB)
	cluster.Querier = querierSizing(ingestionRateMB)
	cluster.QueryFrontend = queryFrontendSizing(ingestionRateMB)
	cluster.Ruler = rulerSizing(ingestionRateMB)
	cluster.Compactor = compactorSizing(ingestionRateMB)
	cluster.ChunksCache = chunksCacheSizing(ingestionRateMB)
	cluster.QueryResultsCache = queryResultsCacheSizing(ingestionRateMB)
	cluster.IndexCache = indexCacheSizing(ingestionRateMB)
	cluster.IndexGateway = indexGatewaySizing(ingestionRateMB)
	return cluster
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

func distributorSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 5.
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// maintain minimum two for HA
	if n < 2 {
		n = 2
	}

	component.Name = Distributor
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("500MB")
	_ = component.Resources.MemoryLimits.Set("1GB")
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(1)
	return component
}

func ingesterSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 2.9
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// maintain minimum 3 for replication
	if n < 3 {
		n = 3
	}

	component.Name = Ingester
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("7GB")
	_ = component.Resources.MemoryLimits.Set("14GB")
	component.Resources.CPURequests.SetCores(1)
	component.Resources.CPULimits.SetCores(2)
	component.Resources.DiskGB = 150
	return component
}

func queryFrontendSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	// run two for HA
	n := 2
	component.Name = QueryFrontend
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("5GB")
	_ = component.Resources.MemoryLimits.Set("10GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(3)
	return component
}

func querierSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 3.5
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// require a minimum number of queriers to be able to reasonably parallelize workloads
	if n < 8 {
		n = 8
	}

	component.Name = Querier
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("6GB")
	_ = component.Resources.MemoryLimits.Set("16GB")
	component.Resources.CPURequests.SetCores(4)
	component.Resources.CPULimits.SetCores(7)
	return component
}

func chunksCacheSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 6.
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	component.Name = ChunksCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("5GB")
	_ = component.Resources.MemoryLimits.Set("6GB")
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return component
}

func queryResultsCacheSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 80.
	n := replicas(MBSecondPerInstance, ingestionRateMB)
	if n < 1 {
		n = 1
	}

	component.Name = QueryResultsCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set(fmt.Sprintf("%dMB", int(1024*1.5)))
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return component
}

func indexCacheSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 17.
	n := replicas(MBSecondPerInstance, ingestionRateMB)
	if n < 1 {
		n = 1
	}

	component.Name = IndexCache
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set(fmt.Sprintf("%dMB", int(1024*1.5)))
	component.Resources.CPURequests.SetCores(0.5)
	component.Resources.CPULimits.SetCores(3)
	return component
}

func rulerSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	// use a constant two rulers for now
	n := 2

	component.Name = Ruler
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("6GB")
	_ = component.Resources.MemoryLimits.Set("16GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(7)
	return component
}

func compactorSizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	n := 1

	component.Name = Compactor
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set("2GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(4)
	component.Resources.DiskGB = 100
	return component
}

func indexGatewaySizing(ingestionRateMB float64) *ComponentDescription {
	component := &ComponentDescription{}
	MBSecondPerInstance := 60.
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// min 2 for HA
	if n < 2 {
		n = 2
	}

	component.Name = IndexGateway
	component.Replicas = n
	_ = component.Resources.MemoryRequests.Set("1GB")
	_ = component.Resources.MemoryLimits.Set("3GB")
	component.Resources.CPURequests.SetCores(2)
	component.Resources.CPULimits.SetCores(2)
	component.Resources.DiskGB = 400
	return component
}

func replicas(perInstance, ingestionRate float64) int {
	return int(math.Ceil(ingestionRate / perInstance))
}
