package main

import (
	"math"

	"github.com/grafana/loki/pkg/validation"
)

func main() {}

type Inputs []validation.Limits

func (xs Inputs) IngestionRate() (rate float64) {
	for _, x := range xs {
		rate += x.IngestionRateMB
	}
	return rate
}

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

func distributorSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 5.e6
	n := replicas(MBSecondPerInstance, ingestionRateMB)

	// maintain minimum two for HA
	if n < 2 {
		n = 2
	}

	memoryGBReq := 0.5
	memoryGBLimits := 1

	cpuCoresReq := 0.5
	cpuCoresLim := 0.5

}

func ingesterSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 2.8
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	// maintain minimum 3 for replication
	if n < 3 {
		n = 3
	}

	memoryGBReq := 7
	memoryGBLimits := 14

	cpuCoresReq := 1
	cpuCoresLim := 2

	diskGB := 150
}

func queryFrontendSizing(ingestionRateMB float64) {
	// run two for HA
	n := 2

	memoryGBReq := 5
	memoryGBLimits := 10

	cpuCoresReq := 2
	cpuCoresLim := 3

}

func querierSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 2.5
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	// require a minimum number of queriers to be able to reasonably parallelize workloads
	if n < 8 {
		n = 8
	}

	memoryGBReq := 6
	memoryGBLimits := 16

	cpuCoresReq := 4
	cpuCoresLim := 7
}

func memcachedChunksSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 2.
	n := replicas(ingestionRateMB, MBSecondPerInstance)

	memoryGBReq := 5
	memoryGBLimits := 6

	cpuCoresReq := 0.5
	cpuCoresLim := 3
}

func memcachedFrontendSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 23.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	memoryGBReq := 1
	memoryGBLimits := 1.5

	cpuCoresReq := 0.5
	cpuCoresLim := 3
}

func memcachedIndexQueriesSizing(ingestionRateMB float64) {
	MBSecondPerInstance := 14.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	memoryGBReq := 1
	memoryGBLimits := 1.5

	cpuCoresReq := 0.5
	cpuCoresLim := 3
}

func rulerIndexQueriesSizing(ingestionRateMB float64) {
	// use a constant two rulers for now
	n := 2

	memoryGBReq := 6
	memoryGBLimits := 16

	cpuCoresReq := 2
	cpuCoresLim := 7
}

func compactorSizing(ingestionRateMB float64) {
	n := 1

	memoryGBReq := 1
	memoryGBLimits := 2

	cpuCoresReq := 2
	cpuCoresLim := 4

	diskGB := 100
}

func indexGatewaySizing(ingestionRateMB float64) {
	MBSecondPerInstance := 45.
	n := replicas(ingestionRateMB, MBSecondPerInstance)
	if n < 1 {
		n = 1
	}

	memoryGBReq := 1
	memoryGBLimits := 3

	cpuCoresReq := 2
	cpuCoresLim := 2

	diskGB := 400
}

func replicas(perInstance, ingestionRate float64) int {
	return int(math.Ceil(ingestionRate / perInstance))
}

// func nodeCount()
// func clusterNodeCount()
