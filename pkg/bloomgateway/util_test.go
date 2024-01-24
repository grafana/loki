package bloomgateway

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func TestGetFromThrough(t *testing.T) {
	chunks := []*logproto.ShortRef{
		{From: 0, Through: 6},
		{From: 1, Through: 5},
		{From: 2, Through: 9},
		{From: 3, Through: 8},
		{From: 4, Through: 7},
	}
	from, through := getFromThrough(chunks)
	require.Equal(t, model.Time(0), from)
	require.Equal(t, model.Time(9), through)

	// assert that slice order did not change
	require.Equal(t, model.Time(0), chunks[0].From)
	require.Equal(t, model.Time(4), chunks[len(chunks)-1].From)
}

func mkBlockRef(minFp, maxFp uint64) bloomshipper.BlockRef {
	return bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			MinFingerprint: minFp,
			MaxFingerprint: maxFp,
		},
	}
}

func TestPartitionFingerprintRange(t *testing.T) {
	seriesPerBound := 100
	bounds := []bloomshipper.BlockRef{
		mkBlockRef(0, 99),
		mkBlockRef(100, 199),
		mkBlockRef(200, 299),
		mkBlockRef(300, 399), // one out of bounds block
	}

	nTasks := 4
	nSeries := 300
	tasks := make([]Task, nTasks)
	for i := 0; i < nSeries; i++ {
		if tasks[i%4].Request == nil {
			tasks[i%4].Request = &logproto.FilterChunkRefRequest{}
		}
		tasks[i%4].Request.Refs = append(tasks[i%nTasks].Request.Refs, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
	}

	results := partitionFingerprintRange(tasks, bounds)
	require.Equal(t, 3, len(results)) // ensure we only return bounds in range
	for _, res := range results {
		// ensure we have the right number of tasks per bound
		for i := 0; i < nTasks; i++ {
			require.Equal(t, seriesPerBound/nTasks, len(res.tasks[i].Request.Refs))
		}
	}

	// ensure bound membership
	for i := 0; i < nSeries; i++ {
		require.Equal(t,
			&logproto.GroupedChunkRefs{Fingerprint: uint64(i)},
			results[i/seriesPerBound].tasks[i%nTasks].Request.Refs[i%seriesPerBound/nTasks],
		)
	}
}
