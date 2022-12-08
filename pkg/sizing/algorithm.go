package sizing

import (
	"math"
)

type ClusterSize struct {
	totalNodes         int
	totalReadReplicas  int
	totalWriteReplicas int

	expectedMaxReadThroughputBytesSec float64
	expectedMaxIngestBytesDay         float64
}

type QueryPerf string

const (
	Basic QueryPerf = "basic"
	Super QueryPerf = "super"
)

func calculateClusterSize(nt NodeType, tbDayIngest int, qperf QueryPerf) ClusterSize {
	// 1 Petabyte per day is maximum
	bytesDayIngest := math.Min(float64(tbDayIngest), 1000.0) * 1e12
	bytesSecondIngest := bytesDayIngest / 86400
	numWriteReplicasNeeded := math.Ceil(bytesSecondIngest / nt.writePod.rateBytesSecond)

	//Hack based on current 4-1 mem to cpu ratio and base machine w/ 4 cores and 1 write/read
	writeReplicasPerNode := float64(nt.cores / 4)
	fullyWritePackedNodes := math.Floor(numWriteReplicasNeeded / writeReplicasPerNode)
	replicasOnLastNode := math.Mod(numWriteReplicasNeeded, writeReplicasPerNode)

	coresOnLastNode := 0.0
	if replicasOnLastNode >= 0.0 {
		coresOnLastNode = math.Max(float64(nt.cores)-replicasOnLastNode*nt.writePod.cpuRequest, 0.0)
	}

	nodesNeededForWrites := math.Ceil(numWriteReplicasNeeded / writeReplicasPerNode)

	// Hack based on packing 1 read and 1 write per node
	readReplicasPerNode := writeReplicasPerNode
	readReplicasOnFullyPackedWriteNodes := readReplicasPerNode * fullyWritePackedNodes
	readReplicasOnPartiallyPackedWriteNodes := math.Floor(coresOnLastNode / nt.readPod.cpuRequest)

	basicQperfReadReplicas := readReplicasOnFullyPackedWriteNodes + readReplicasOnPartiallyPackedWriteNodes

	scaleUp := 0.25
	additionalReadReplicas := 0.0
	if qperf != Basic {
		additionalReadReplicas = basicQperfReadReplicas * scaleUp
	}

	readReplicasPerEmptyNode := math.Floor(float64(nt.cores) / nt.readPod.cpuRequest)
	additionalNodesNeededForReads := additionalReadReplicas / readReplicasPerEmptyNode

	actualNodesAddedForReads := calculateActualReadNodes(additionalNodesNeededForReads)
	actualReadReplicasAdded := actualNodesAddedForReads * readReplicasPerEmptyNode

	totalReadReplicas := actualReadReplicasAdded + basicQperfReadReplicas
	totalReadThroughputBytesSec := totalReadReplicas * nt.readPod.rateBytesSecond

	totalNodesNeeded := nodesNeededForWrites + actualNodesAddedForReads

	return ClusterSize{
		totalNodes:         int(totalNodesNeeded),
		totalReadReplicas:  int(totalReadReplicas),
		totalWriteReplicas: int(numWriteReplicasNeeded),

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
