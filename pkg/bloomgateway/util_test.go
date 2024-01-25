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
	t.Run("consecutive block ranges", func(t *testing.T) {
		bounds := []bloomshipper.BlockRef{
			mkBlockRef(0, 99),    // out of bounds block
			mkBlockRef(100, 199), // contains partially [150..199]
			mkBlockRef(200, 299), // contains fully [200..299]
			mkBlockRef(300, 399), // contains partially [300..349]
			mkBlockRef(400, 499), // out of bounds block
		}

		nTasks := 5
		nSeries := 200
		startFp := 150

		tasks := make([]Task, nTasks)
		for i := startFp; i < startFp+nSeries; i++ {
			if tasks[i%nTasks].Request == nil {
				tasks[i%nTasks].Request = &logproto.FilterChunkRefRequest{}
			}
			tasks[i%nTasks].Request.Refs = append(tasks[i%nTasks].Request.Refs, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
		}

		results := partitionFingerprintRange(tasks, bounds)
		require.Equal(t, 3, len(results)) // ensure we only return bounds in range

		actualFingerprints := make([]*logproto.GroupedChunkRefs, 0, nSeries)
		expectedTaskRefs := []int{10, 20, 10}
		for i, res := range results {
			// ensure we have the right number of tasks per bound
			require.Len(t, res.tasks, 5)
			for _, task := range res.tasks {
				require.Equal(t, expectedTaskRefs[i], len(task.Request.Refs))
				actualFingerprints = append(actualFingerprints, task.Request.Refs...)
			}
		}

		// ensure bound membership
		expectedFingerprints := make([]*logproto.GroupedChunkRefs, nSeries)
		for i := 0; i < nSeries; i++ {
			expectedFingerprints[i] = &logproto.GroupedChunkRefs{Fingerprint: uint64(startFp + i)}
		}

		require.ElementsMatch(t, expectedFingerprints, actualFingerprints)
	})

	t.Run("inconsecutive block ranges", func(t *testing.T) {
		bounds := []bloomshipper.BlockRef{
			mkBlockRef(0, 89),
			mkBlockRef(100, 189),
			mkBlockRef(200, 289),
		}

		task := Task{Request: &logproto.FilterChunkRefRequest{}}
		for i := 0; i < 300; i++ {
			task.Request.Refs = append(task.Request.Refs, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
		}

		results := partitionFingerprintRange([]Task{task}, bounds)
		require.Equal(t, 3, len(results)) // ensure we only return bounds in range
		for _, res := range results {
			// ensure we have the right number of tasks per bound
			require.Len(t, res.tasks, 1)
			require.Len(t, res.tasks[0].Request.Refs, 90)
		}
	})
}
