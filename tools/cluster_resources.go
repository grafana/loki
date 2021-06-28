package //insert some name

type componentName int
const (
	Distributor componentName = iota
	Ingester
	Querier
	QueryFrontend
	Ruler
	Compactor 
	ChunksCache //memcached instance
	QueryResultsCache //memcached instance
	IndexCache //memcached instance
	IndexGateway 
	NumComponents //Leave this as last - it tells you the number of components to expect
)

//This is ugly. 
func componentNameString(cn componentName) string{
	if (cn == Distributor){
		return "Distributor"
	} else if (cn == Ingester){
		return "Ingester"
	} else if (cn == Querier){
		return "Querier"
	} else if (cn == QueryFrontend){
		return "QueryFrontend"
	} else if (cn == Ruler){
		return "Ruler"
	} else if (cn == Compactor){
		return "Compactor"
	} else if (cn == ChunksCache){
		return "ChunksCache"
	} else if (cn == QueryResultsCache){
		return "QueryResultsCache"
	} else if (cn == IndexCache){
		return "IndexCache"
	} else if (cn == IndexGateway){
		return "IndexGateway"
	}
	return "Unrecognized Component" //should really be throwing an error here
}

type clusterResources struct {
	//all the components in a cluster are contained in this array
	//it is sized fo the number of components we expect for a Loki cluster
	componentArray [NumComponents]componentDescription
	
	//cpus, memory, and disk for the whole cluster
	clusterComputeResources computeResources

	//number of nodes required in the k8s cluster being deployed to
	//should be the ceiling of # of ingesters required and # of queriers required since we only deploy
	//one of each of these per node
	//for now, we're going to pass on specifying the size of each node and just assume they're "reasonably" sized
	numNodes int

	//TODO: We should probably also store the user's inputs here, like volume of logs per day
}

type componentDescription struct {
	componentComputeResources computeResources // cpu, mem, and disk requirements for a single instance of this component
	replicas int //how many copies of this component I'll be running
	mycomponentName componentName //identifies the component for which I'm storing the resources
}

type computeResources struct {
	//Limit is the max resources that we'd allocate to this; its the ceiling of what its able to consume
	//Request is the minimum resources that we'd need to schedule this 
	numCpus_limit int
	numCpus_request int

	mbMemory_limit int
	mbMemory_request int

	gbDisk_limit int
	gbDisk_request int
}

//QUESTION: Not sure if Owen already plans to output these values at a cluster level
//We may not need this function
func calcClusterResources(c *clusterResources) {
	cpu_request, cpu_limit, mem_request, mem_limit, disk_request, disk_limit := 0, 0, 0, 0, 0, 0

	//loop through all components in the cluster; multiply resource usage for each individual instance of a component
	//by the number of replicas to get the total resource usage for that component
	//add that together for all components. 
	for _, component := range c.componentArray {
		cpu_request += (component.componentComputeResources.numCpus_request * component.replicas)
		cpu_limit += (component.componentComputeResources.numCpus_limit * component.replicas)

		mem_request += (component.componentComputeResources.mbMemory_request * component.replicas)
		mem_limit += (component.componentComputeResources.mbMemory_limit * component.replicas)

		disk_request += (component.componentComputeResources.gbDisk_request * component.replicas)
		disk_limit += (component.componentComputeResources.gbDisk_limit * component.replicas)
	}

	c.clusterComputeResources.numCpus_request = cpu_request
	c.clusterComputeResources.numCpus_limit = cpu_limit

	c.clusterComputeResources.mbMemory_request = mem_request
	c.clusterComputeResources.mbMemory_limit = mem_limit
	
	c.clusterComputeResources.gbDisk_request = disk_request
	c.clusterComputeResources.gbDisk_limit = disk_limit
}

//TODO: Add verbose flag to include the "request" (min resources) in addition to "limit" (max resources)
func printClusterArchitecture(c *clusterResources){

	//loop through all components, and print out how many replicas of each component we're recommending. 
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

	//TODO: Actually populate the value of X volume of ingest
	fmt.Println("Overall Requirements for a Loki cluster than can handle X volume of ingest")
	fmt.Printf("\tNumber of Nodes: %d\n", c.numNodes)
	fmt.Printf("\tMemory Required: %d MB\n", c.clusterComputeResources.mbMemory_limit)
	fmt.Printf("\tCPUs Required: %d\n", c.clusterComputeResources.numCpus_limit)
	fmt.Printf("\tDisk Required: %d GB\n", c.clusterComputeResources.gbDisk_limit)

	fmt.Printf("\n")

	fmt.Printf("List of all components in the Loki cluster, the number of replicas of each, and the resources required per replica\n")

	for _, component := range c.componentArray {
		fmt.Printf("%s: %d replicas, each of which requires\n", componentNameString(component.mycomponentName), component.replicas)
		fmt.Printf("\t%d MB of memory\n", component.componentComputeResources.mbMemory_limit)
		fmt.Printf("\t%d CPUs\n", component.componentComputeResources.numCpus_limit)
		fmt.Printf("\t%d GB of disk\n", component.componentComputeResources.gbDisk_limit)
	}


}

