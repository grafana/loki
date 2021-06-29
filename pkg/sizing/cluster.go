package sizing // insert some name

import (
	"fmt"

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
	ChunksCache // memcached instance

	QueryResultsCache // memcached instance
	IndexCache        // memcached instance
	IndexGateway
	NumComponents // Leave this as last - it tells you the number of components to expect
)

// This is ugly.
func ComponentNameString(cn ComponentName) string {
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
	ComponentComputeResources ComputeResources // cpu, mem, and disk requirements for a single instance of this component
	Replicas                  int              // how many copies of this component I'll be running
	Name                      ComponentName    // identifies the component for which I'm storing the resources
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

		compute.CPURequests += component.ComponentComputeResources.CPURequests * CPUSize(component.Replicas)
		compute.CPULimits += component.ComponentComputeResources.CPULimits * CPUSize(component.Replicas)

		compute.MemoryRequests += component.ComponentComputeResources.MemoryRequests * flagext.ByteSize(component.Replicas)
		compute.MemoryLimits += component.ComponentComputeResources.MemoryLimits * flagext.ByteSize(component.Replicas)

		compute.DiskGB += (component.ComponentComputeResources.DiskGB * component.Replicas)
	}
	return compute

}

// TODO: Add verbose flag to include the "request" (min resources) in addition to "limit" (max resources)
func printClusterArchitecture(c *ClusterResources) {

	// loop through all components, and print out how many replicas of each component we're recommending.
	/*
		Format will look like
		"""
		Overall Requirements for a Loki cluster than can handle X volume of ingest
		Number of Nodes: 2
		Memory Required: 1000 MB
		CPUs Required: 34
		Disk Required: 100 GB

		List of all components in the Loki cluster, the number of replicas of each, and the resources required per replica

		Ingester: 5 replicas, each with:
			2000 MB RAM
			10 GB Disk
			5 CPU

		Distributor: 2 replicas, each with:
			1000 MB RAM
			1 GB Disk
			2 CPU
		"""
	*/

	totals := c.Totals()

	// TODO: Actually populate the value of X volume of ingest
	fmt.Println("Overall Requirements for a Loki cluster than can handle X volume of ingest")
	fmt.Printf("\tNumber of Nodes: %d\n", c.NumNodes())
	fmt.Printf("\tMemory Required: %d MB\n", totals.MemoryLimits)
	fmt.Printf("\tCPUs Required: %d\n", totals.CPULimits)
	fmt.Printf("\tDisk Required: %d GB\n", totals.DiskGB)

	fmt.Printf("\n")

	fmt.Printf("List of all components in the Loki cluster, the number of replicas of each, and the resources required per replica\n")

	for _, component := range c.Components() {
		if component != nil {
			fmt.Printf("%s: %d replicas, each of which requires\n", ComponentNameString(component.Name), component.Replicas)
			fmt.Printf("\t%v MB of memory\n", component.ComponentComputeResources.MemoryLimits)
			fmt.Printf("\t%v CPUs\n", component.ComponentComputeResources.CPULimits)
			fmt.Printf("\t%d GB of disk\n", component.ComponentComputeResources.DiskGB)
		}
	}

}
