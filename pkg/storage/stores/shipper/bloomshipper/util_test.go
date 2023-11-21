package bloomshipper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func mkBlockRef(minFp, maxFp uint64) BlockRef {
	return BlockRef{
		Ref: Ref{
			MinFingerprint: minFp,
			MaxFingerprint: maxFp,
		},
	}
}

func TestPartitionFingerprintRange(t *testing.T) {
	seriesPerBound := 100
	bounds := []BlockRef{
		mkBlockRef(0, 99),
		mkBlockRef(100, 199),
		mkBlockRef(200, 299),
		mkBlockRef(300, 399), // one out of bounds block
	}

	nReqs := 4
	nSeries := 300
	reqs := make([][]*logproto.GroupedChunkRefs, nReqs)
	for i := 0; i < nSeries; i++ {
		reqs[i%4] = append(reqs[i%nReqs], &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
	}

	results := PartitionFingerprintRange(reqs, bounds)
	require.Equal(t, 3, len(results)) // ensure we only return bounds in range
	for _, res := range results {
		// ensure we have the right number of requests per bound
		for i := 0; i < nReqs; i++ {
			require.Equal(t, seriesPerBound/nReqs, len(res.Refs[i]))
		}
	}

	// ensure bound membership
	for i := 0; i < nSeries; i++ {
		require.Equal(t,
			&logproto.GroupedChunkRefs{Fingerprint: uint64(i)},
			results[i/seriesPerBound].Refs[i%nReqs][i%seriesPerBound/nReqs],
		)
	}

}
