package sizing // insert some name

import (
	"math"

	"github.com/grafana/loki/pkg/util/flagext"
)

type ComponentName int

const (
	Distributor ComponentName = iota
	Ingester
	Querier
	QueryFrontend
	Ruler
	Compactor
	ChunksCache       // memcached instance
	QueryResultsCache // memcached instance
	IndexCache        // memcached instance
	IndexGateway
	NumComponents // Leave this as last - it tells you the number of components to expect
)

// This is ugly.
func (cn ComponentName) String() string {
	switch cn {
	case Distributor:
		return "Distributor"
	case Ingester:
		return "Ingester"
	case Querier:
		return "Querier"
	case QueryFrontend:
		return "QueryFrontend"
	case Ruler:
		return "Ruler"
	case Compactor:
		return "Compactor"
	case ChunksCache:
		return "ChunksCache"
	case QueryResultsCache:
		return "QueryResultsCache"
	case IndexCache:
		return "IndexCache"
	case IndexGateway:
		return "IndexGateway"
	default:
		return "Unrecognized Component" // should really be throwing an error here
	}
}

type ClusterResources struct {
	Distributor,
	Ingester,
	Querier,
	QueryFrontend,
	Ruler,
	Compactor,
	ChunksCache,
	QueryResultsCache,
	IndexCache,
	IndexGateway *ComponentDescription
}

func (r *ClusterResources) Components() []*ComponentDescription {
	return []*ComponentDescription{
		r.Distributor,
		r.Ingester,
		r.Querier,
		r.QueryFrontend,
		r.Ruler,
		r.Compactor,
		r.ChunksCache,
		r.QueryResultsCache,
		r.IndexCache,
		r.IndexGateway,
	}
}

func (r *ClusterResources) NumNodes() (n int) {
	// number of nodes required in the k8s cluster being deployed to
	// should be the ceiling of # of ingesters required and # of queriers required since we only deploy
	// one of each of these per node
	// For now, we're going to pass on specifying the size of each node and just assume they're "reasonably" sized
	for _, c := range r.Components() {
		if c != nil && n < c.Replicas {
			n = c.Replicas
		}
	}
	return n
}

type ComponentDescription struct {
	Resources ComputeResources // cpu, mem, and disk requirements for a single instance of this component
	Replicas  int              // how many copies of this component I'll be running
	Name      ComponentName    // identifies the component for which I'm storing the resources
}

type ComputeResources struct {
	// Limit is the max resources that we'd allocate to this; its the ceiling of what its able to consume
	// Request is the minimum resources that we'd need to schedule this
	CPURequests CPUSize
	CPULimits   CPUSize

	MemoryRequests flagext.ByteSize
	MemoryLimits   flagext.ByteSize

	DiskGB int
}

// QUESTION: Not sure if Owen already plans to output these values at a cluster level
// We may not need this function
func (r *ClusterResources) Totals() ComputeResources {
	var compute ComputeResources

	// loop through all components in the cluster; multiply resource usage for each individual instance of a component
	// by the number of Replicas to get the total resource usage for that component
	// add that together for all components.
	for _, component := range r.Components() {
		if component == nil {
			continue
		}

		compute.CPURequests += (component.Resources.CPURequests * CPUSize(component.Replicas))
		compute.CPULimits += (component.Resources.CPULimits * CPUSize(component.Replicas))

		compute.MemoryRequests += (component.Resources.MemoryRequests * flagext.ByteSize(component.Replicas))
		compute.MemoryLimits += (component.Resources.MemoryLimits * flagext.ByteSize(component.Replicas))

		compute.DiskGB += (component.Resources.DiskGB * component.Replicas)
	}
	return compute

}

func ComputeObjectStorage(IngestRate flagext.ByteSize, DaysRetention int) int {
	PerSecIngestionRateMB := float64(IngestRate.Val()) / (1 << 20)

	compressionRate := 0.15 //This means we assume logs are compressed to 15% of their original size
	secondsInDay := 86400

	TBstoragerequired := (PerSecIngestionRateMB * float64(secondsInDay) * float64(DaysRetention) * compressionRate) / (1024.0 * 1024.0)

	return int(math.Ceil(TBstoragerequired))

}
