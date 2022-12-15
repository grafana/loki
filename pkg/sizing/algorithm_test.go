package sizing

import (
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/require"
)

func Test_AlgorithTest_Algorith(t *testing.T) {
	f := func(ingest int) bool {
		if ingest < 0 {
			ingest = -ingest
		}
		postiveReplicas := true
		for _, cloud := range NodeTypesByProvider {
			for _, node := range cloud {
				size := calculateClusterSize(node, ingest, Basic)
				postiveReplicas = size.totalNodes > 0.0 && size.totalReadReplicas > 0.0 && size.totalWriteReplicas > 0.0
				require.Truef(t, postiveReplicas, "Cluster size was empty: ingest=%d cluster=%v node=%v", ingest, size, node)
				require.InDelta(t, size.totalReadReplicas, size.totalWriteReplicas, 5.0, "Replicas have different sizes: ingest=%d node=%s", ingest, node.name)

				size = calculateClusterSize(node, ingest, Super)
				postiveReplicas = size.totalNodes > 0.0 && size.totalReadReplicas > 0.0 && size.totalWriteReplicas > 0.0
				require.Truef(t, postiveReplicas, "Cluster size was empty: ingest=%d cluster=%v node=%v", ingest, size, node)
			}
		}

		return postiveReplicas
	}

	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}
