package sizing

import (
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/require"
)

func Test_AlgorithTest_Algorithm(t *testing.T) {
	f := func(ingest int) bool {
		if ingest < 0 {
			ingest = -ingest
		}
		postiveReplicas := true
		for _, cloud := range NodeTypesByProvider {
			for _, node := range cloud {
				size := calculateClusterSize(node, ingest, Basic)
				postiveReplicas = size.TotalNodes > 0.0 && size.TotalReadReplicas > 0.0 && size.TotalWriteReplicas > 0.0
				require.Truef(t, postiveReplicas, "Cluster size was empty: ingest=%d cluster=%v node=%v", ingest, size, node)
				require.InDelta(t, size.TotalReadReplicas, size.TotalWriteReplicas, 5.0, "Replicas have different sizes: ingest=%d node=%s", ingest, node.name)

				size = calculateClusterSize(node, ingest, Super)
				postiveReplicas = size.TotalNodes > 0.0 && size.TotalReadReplicas > 0.0 && size.TotalWriteReplicas > 0.0
				require.Truef(t, postiveReplicas, "Cluster size was empty: ingest=%d cluster=%v node=%v", ingest, size, node)
			}
		}

		return postiveReplicas
	}

	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func Test_CoresNodeInvariant(t *testing.T) {
	for _, queryPerformance := range []QueryPerf{Basic, Super} {
		for _, ingest := range []int{1, 2} {
			for _, cloud := range NodeTypesByProvider {
				for _, node := range cloud {
					size := calculateClusterSize(node, ingest, queryPerformance)
					require.LessOrEqualf(t, size.TotalCoresLimit, float64(size.TotalNodes*node.cores), "given ingest=%d node=%s total cores must be less than available cores", ingest, node.name)
				}
			}
		}
	}
}
