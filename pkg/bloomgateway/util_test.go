package bloomgateway

import (
	"testing"
	"time"

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

func TestTruncateDay(t *testing.T) {
	expected := mktime("2024-01-24 00:00")

	for _, inp := range []string{
		"2024-01-24 00:00",
		"2024-01-24 08:00",
		"2024-01-24 16:00",
		"2024-01-24 23:59",
	} {
		t.Run(inp, func(t *testing.T) {
			ts := mktime(inp)
			result := truncateDay(ts)
			require.Equal(t, expected, result)
		})
	}
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
			tasks[i%nTasks].series = append(tasks[i%nTasks].series, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
		}

		results := partitionFingerprintRange(tasks, bounds)
		require.Equal(t, 3, len(results)) // ensure we only return bounds in range

		actualFingerprints := make([]*logproto.GroupedChunkRefs, 0, nSeries)
		expectedTaskRefs := []int{10, 20, 10}
		for i, res := range results {
			// ensure we have the right number of tasks per bound
			require.Len(t, res.tasks, 5)
			for _, task := range res.tasks {
				require.Equal(t, expectedTaskRefs[i], len(task.series))
				actualFingerprints = append(actualFingerprints, task.series...)
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

		task := Task{}
		for i := 0; i < 300; i++ {
			task.series = append(task.series, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
		}

		results := partitionFingerprintRange([]Task{task}, bounds)
		require.Equal(t, 3, len(results)) // ensure we only return bounds in range
		for _, res := range results {
			// ensure we have the right number of tasks per bound
			require.Len(t, res.tasks, 1)
			require.Len(t, res.tasks[0].series, 90)
		}
	})
}

func TestPartitionRequest(t *testing.T) {
	ts := mktime("2024-01-24 12:00")

	testCases := map[string]struct {
		inp *logproto.FilterChunkRefRequest
		exp []seriesWithBounds
	}{

		"empty": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-24 * time.Hour),
				Through: ts,
			},
			exp: []seriesWithBounds{},
		},

		"all chunks within single day": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-1 * time.Hour),
				Through: ts,
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-60 * time.Minute), Through: ts.Add(-50 * time.Minute)},
						},
					},
					{
						Fingerprint: 0x01,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-55 * time.Minute), Through: ts.Add(-45 * time.Minute)},
						},
					},
				},
			},
			exp: []seriesWithBounds{
				{
					bounds: model.Interval{Start: ts.Add(-60 * time.Minute), End: ts.Add(-45 * time.Minute)},
					day:    mktime("2024-01-24 00:00"),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-60 * time.Minute), Through: ts.Add(-50 * time.Minute)},
							},
						},
						{
							Fingerprint: 0x01,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-55 * time.Minute), Through: ts.Add(-45 * time.Minute)},
							},
						},
					},
				},
			},
		},

		"chunks across multiple days - no overlap": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-24 * time.Hour),
				Through: ts,
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-23 * time.Hour), Through: ts.Add(-22 * time.Hour)},
						},
					},
					{
						Fingerprint: 0x01,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-2 * time.Hour), Through: ts.Add(-1 * time.Hour)},
						},
					},
				},
			},
			exp: []seriesWithBounds{
				{
					bounds: model.Interval{Start: ts.Add(-23 * time.Hour), End: ts.Add(-22 * time.Hour)},
					day:    mktime("2024-01-23 00:00"),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-23 * time.Hour), Through: ts.Add(-22 * time.Hour)},
							},
						},
					},
				},
				{
					bounds: model.Interval{Start: ts.Add(-2 * time.Hour), End: ts.Add(-1 * time.Hour)},
					day:    mktime("2024-01-24 00:00"),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x01,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-2 * time.Hour), Through: ts.Add(-1 * time.Hour)},
							},
						},
					},
				},
			},
		},

		"chunks across multiple days - overlap": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-24 * time.Hour),
				Through: ts,
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)},
						},
					},
				},
			},
			exp: []seriesWithBounds{
				{
					bounds: model.Interval{Start: ts.Add(-13 * time.Hour), End: ts.Add(-11 * time.Hour)},
					day:    mktime("2024-01-23 00:00"),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)},
							},
						},
					},
				},
				{
					bounds: model.Interval{Start: ts.Add(-13 * time.Hour), End: ts.Add(-11 * time.Hour)},
					day:    mktime("2024-01-24 00:00"),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			result := partitionRequest(tc.inp)
			require.Equal(t, tc.exp, result)
		})
	}

}
