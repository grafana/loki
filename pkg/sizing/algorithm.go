package sizing

import (
	"math"
)

type ClusterSize struct {
	totalNodes         int
	totalReadReplicas  int
	totalWriteReplicas int

	expectedMaxReadThroughputGbSec float64
	expectedMaxIngestTbDay         float64
}

type QueryPerf string

const (
	Basic QueryPerf = "basic"
	Super QueryPerf = "super"
)

func calculateClusterSize(nt NodeType, tbDayIngest int, qperf QueryPerf) ClusterSize {

	mbDayIngest := float64(tbDayIngest) * 1e6
	mbSecondIngest := mbDayIngest / 86400
	numWriteReplicasNeeded := math.Ceil(mbSecondIngest / nt.writePod.rateMbSecond)

	//Hack based on current 4-1 mem to cpu ratio and base machine w/ 4 cores and 1 write/read
	writeReplicasPerNode := float64(nt.cores / 4)
	fullyWritePackedNodes := math.Floor(numWriteReplicasNeeded / writeReplicasPerNode)
	replicasOnLastNode := math.Mod(numWriteReplicasNeeded, writeReplicasPerNode)

	coresOnLastNode := 0.0
	if replicasOnLastNode >= 0.0 {
		coresOnLastNode = float64(nt.cores) - replicasOnLastNode*nt.writePod.cpuRequest
	}
	//Commenting this out because we don't actually need it since we're cpu bound more than anything
	//const mem_on_last_node = (replicas_on_last_node === 0) ? 0 : nt.memory - replicas_on_last_node*nt.writePod.memoryRequest

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
	totalReadThroughput := (totalReadReplicas * nt.readPod.rateMbSecond) / 1e3

	totalNodesNeeded := nodesNeededForWrites + actualNodesAddedForReads

	return ClusterSize{
		totalNodes:         int(totalNodesNeeded),
		totalReadReplicas:  int(totalReadReplicas),
		totalWriteReplicas: int(numWriteReplicasNeeded),

		expectedMaxReadThroughputGbSec: totalReadThroughput,
		expectedMaxIngestTbDay:         ((nt.writePod.rateMbSecond * numWriteReplicasNeeded) / 1e6) * 86400,
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
