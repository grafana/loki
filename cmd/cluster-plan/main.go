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
// Cluster svcs: ruler, Compactor, (table manager - ignore and assume we're using the new retention)
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
	MBSecondPerDistributor := 5.e6
	n := replicas(MBSecondPerDistributor, ingestionRateMB)

	memoryGBReq := 0.5
	memoryGBLimits := 1

	cpuCoresReq := 0.5
	cpuCoresLim := 0.5

}

func ingesterSizing(ingestionRateMB float64) {
	MBSecondPerIngester := 2.8
	n := replicas(ingestionRateMB, MBSecondPerIngester)

	memoryGBReq := 7
	memoryGBLimits := 14

	cpuCoresReq := 1
	cpuCoresLim := 2

}

func replicas(perInstance, ingestionRate float64) int {
	return int(math.Ceil(ingestionRate / perInstance))
}

// func nodeCount()
// func clusterNodeCount()
