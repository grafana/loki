package sizing

import (
	"math"
)

type ClusterSize struct {
	TotalNodes         int
	TotalReadReplicas  int
	TotalWriteReplicas int
	TotalCoresRequest  float64
	TotalMemoryRequest int

	expectedMaxReadThroughputBytesSec float64
	expectedMaxIngestBytesDay         float64
}

type QueryPerf string

const (
	Basic QueryPerf = "basic"
	Super QueryPerf = "super"
)

func calculateClusterSize(nt NodeType, bytesDayIngest float64, qperf QueryPerf) ClusterSize {

	// 1 Petabyte per day is maximum. We use decimal prefix https://en.wikipedia.org/wiki/Binary_prefix
	bytesDayIngest = math.Min(bytesDayIngest, 1e15)
	bytesSecondIngest := bytesDayIngest / 86400
	numWriteReplicasNeeded := math.Ceil(bytesSecondIngest / nt.writePod.rateBytesSecond)

	// High availability requires at least 3 replicas.
	numWriteReplicasNeeded = math.Max(3, numWriteReplicasNeeded)

	//Hack based on current 4-1 mem to cpu ratio and base machine w/ 4 cores and 1 write/read
	writeReplicasPerNode := float64(nt.cores / 4)
	fullyWritePackedNodes := math.Floor(numWriteReplicasNeeded / writeReplicasPerNode)
	replicasOnLastNode := math.Mod(numWriteReplicasNeeded, writeReplicasPerNode)

	coresOnLastNode := 0.0
	if replicasOnLastNode > 0.0 {
		coresOnLastNode = math.Max(float64(nt.cores)-replicasOnLastNode*nt.writePod.cpuRequest, 0.0)
	}

	nodesNeededForWrites := math.Ceil(numWriteReplicasNeeded / writeReplicasPerNode)

	// Hack based on packing 1 read and 1 write per node
	readReplicasPerNode := writeReplicasPerNode
	readReplicasOnFullyPackedWriteNodes := readReplicasPerNode * fullyWritePackedNodes
	readReplicasOnPartiallyPackedWriteNodes := math.Floor(coresOnLastNode / nt.readPod.cpuRequest)

	// Required read replicase without considering required query performance.
	baselineReadReplicas := readReplicasOnFullyPackedWriteNodes + readReplicasOnPartiallyPackedWriteNodes

	scaleUp := 0.25
	additionalReadReplicas := 0.0
	if qperf != Basic {
		additionalReadReplicas = baselineReadReplicas * scaleUp
	}

	readReplicasPerEmptyNode := math.Floor(float64(nt.cores) / nt.readPod.cpuRequest)
	additionalNodesNeededForReads := additionalReadReplicas / readReplicasPerEmptyNode

	actualNodesAddedForReads := calculateActualReadNodes(additionalNodesNeededForReads)
	actualReadReplicasAdded := actualNodesAddedForReads * readReplicasPerEmptyNode

	totalReadReplicas := actualReadReplicasAdded + baselineReadReplicas

	// High availability requires at least 3 replicas.
	totalReadReplicas = math.Max(3, totalReadReplicas)

	totalReadThroughputBytesSec := totalReadReplicas * nt.readPod.rateBytesSecond

	totalNodesNeeded := nodesNeededForWrites + actualNodesAddedForReads
	totalCoresRequest := numWriteReplicasNeeded*nt.writePod.cpuRequest + totalReadReplicas*nt.readPod.cpuRequest
	totalMemoryRequest := numWriteReplicasNeeded*float64(nt.writePod.memoryRequest) + totalReadReplicas*float64(nt.readPod.memoryRequest)

	return ClusterSize{
		TotalNodes:         int(totalNodesNeeded),
		TotalReadReplicas:  int(totalReadReplicas),
		TotalWriteReplicas: int(numWriteReplicasNeeded),
		TotalCoresRequest:  totalCoresRequest,
		TotalMemoryRequest: int(totalMemoryRequest),

		expectedMaxReadThroughputBytesSec: totalReadThroughputBytesSec,
		expectedMaxIngestBytesDay:         (nt.writePod.rateBytesSecond * numWriteReplicasNeeded) * 86400,
	}
}

func calculateActualReadNodes(additionalNodesNeededForReads float64) float64 {
	if additionalNodesNeededForReads == 0.0 {
		return 0
	}
	if 0.0 < additionalNodesNeededForReads && additionalNodesNeededForReads < 1.0 {
		return 1
	}
	return math.Floor(additionalNodesNeededForReads)
}
