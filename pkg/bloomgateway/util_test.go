package bloomgateway

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func mktime(s string) model.Time {
	ts, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return model.TimeFromUnix(ts.Unix())
}

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
			Bounds: v1.NewBounds(model.Fingerprint(minFp), model.Fingerprint(maxFp)),
		},
	}
}

func TestPartitionTasks(t *testing.T) {

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

		results := partitionTasks(tasks, bounds)
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

		results := partitionTasks([]Task{task}, bounds)
		require.Equal(t, 3, len(results)) // ensure we only return bounds in range
		for _, res := range results {
			// ensure we have the right number of tasks per bound
			require.Len(t, res.tasks, 1)
			require.Len(t, res.tasks[0].series, 90)
		}
	})

	t.Run("block series before and after task series", func(t *testing.T) {
		bounds := []bloomshipper.BlockRef{
			mkBlockRef(100, 200),
		}

		tasks := []Task{
			{
				series: []*logproto.GroupedChunkRefs{
					{Fingerprint: 50},
					{Fingerprint: 75},
					{Fingerprint: 250},
					{Fingerprint: 300},
				},
			},
		}

		results := partitionTasks(tasks, bounds)
		require.Len(t, results, 0)
	})
}

func TestPartitionRequest(t *testing.T) {
	ts := mktime("2024-01-24 12:00")

	testCases := map[string]struct {
		inp *logproto.FilterChunkRefRequest
		exp []seriesWithInterval
	}{

		"empty": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-24 * time.Hour),
				Through: ts,
			},
			exp: []seriesWithInterval{},
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
			exp: []seriesWithInterval{
				{
					interval: bloomshipper.Interval{Start: ts.Add(-60 * time.Minute), End: ts.Add(-45 * time.Minute)},
					day:      config.NewDayTime(mktime("2024-01-24 00:00")),
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
			exp: []seriesWithInterval{
				{
					interval: bloomshipper.Interval{Start: ts.Add(-23 * time.Hour), End: ts.Add(-22 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-23 00:00")),
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
					interval: bloomshipper.Interval{Start: ts.Add(-2 * time.Hour), End: ts.Add(-1 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-24 00:00")),
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
							{From: ts.Add(-14 * time.Hour), Through: ts.Add(-13 * time.Hour)},
							{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)},
							{From: ts.Add(-11 * time.Hour), Through: ts.Add(-10 * time.Hour)},
						},
					},
				},
			},
			exp: []seriesWithInterval{
				{
					interval: bloomshipper.Interval{Start: ts.Add(-14 * time.Hour), End: ts.Add(-11 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-23 00:00")),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-14 * time.Hour), Through: ts.Add(-13 * time.Hour)},
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)},
							},
						},
					},
				},
				{
					interval: bloomshipper.Interval{Start: ts.Add(-11 * time.Hour), End: ts.Add(-10 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-24 00:00")),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-11 * time.Hour), Through: ts.Add(-10 * time.Hour)},
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

			// Run each partition throught the same partitioning process again to make sure results are still the same
			for i := range result {
				result2 := partitionSeriesByDay(result[i].interval.Start, result[i].interval.End, result[i].series)
				require.Len(t, result2, 1)
				require.Equal(t, result[i], result2[0])
			}
		})
	}
}

func createBlocks(t *testing.T, tenant string, n int, from, through model.Time, minFp, maxFp model.Fingerprint) ([]bloomshipper.BlockRef, []bloomshipper.Meta, []*bloomshipper.CloseableBlockQuerier, [][]v1.SeriesWithBloom) {
	t.Helper()

	blockRefs := make([]bloomshipper.BlockRef, 0, n)
	metas := make([]bloomshipper.Meta, 0, n)
	queriers := make([]*bloomshipper.CloseableBlockQuerier, 0, n)
	series := make([][]v1.SeriesWithBloom, 0, n)

	step := (maxFp - minFp) / model.Fingerprint(n)
	for i := 0; i < n; i++ {
		fromFp := minFp + (step * model.Fingerprint(i))
		throughFp := fromFp + step - 1
		// last block needs to include maxFp
		if i == n-1 {
			throughFp = maxFp
		}
		ref := bloomshipper.Ref{
			TenantID:       tenant,
			TableName:      config.NewDayTable(config.NewDayTime(truncateDay(from)), "").Addr(),
			Bounds:         v1.NewBounds(fromFp, throughFp),
			StartTimestamp: from,
			EndTimestamp:   through,
		}
		blockRef := bloomshipper.BlockRef{
			Ref: ref,
		}
		meta := bloomshipper.Meta{
			MetaRef: bloomshipper.MetaRef{
				Ref: ref,
			},
			Blocks: []bloomshipper.BlockRef{blockRef},
		}
		block, data, _ := v1.MakeBlock(t, n, fromFp, throughFp, from, through)
		// Printing fingerprints and the log lines of its chunks comes handy for debugging...
		// for i := range keys {
		// 	t.Log(data[i].Series.Fingerprint)
		// 	for j := range keys[i] {
		// 		t.Log(i, j, string(keys[i][j]))
		// 	}
		// }
		querier := &bloomshipper.CloseableBlockQuerier{
			BlockQuerier: v1.NewBlockQuerier(block, false, v1.DefaultMaxPageSize),
			BlockRef:     blockRef,
		}
		queriers = append(queriers, querier)
		metas = append(metas, meta)
		blockRefs = append(blockRefs, blockRef)
		series = append(series, data)
	}
	return blockRefs, metas, queriers, series
}

func createQueryInputFromBlockData(t *testing.T, tenant string, data [][]v1.SeriesWithBloom, nthSeries int) []*logproto.ChunkRef {
	t.Helper()
	n := 0
	res := make([]*logproto.ChunkRef, 0)
	for i := range data {
		for j := range data[i] {
			if n%nthSeries == 0 {
				chk := data[i][j].Series.Chunks[0]
				res = append(res, &logproto.ChunkRef{
					Fingerprint: uint64(data[i][j].Series.Fingerprint),
					UserID:      tenant,
					From:        chk.From,
					Through:     chk.Through,
					Checksum:    chk.Checksum,
				})
			}
			n++
		}
	}
	return res
}

func createBlockRefsFromBlockData(t *testing.T, tenant string, data []*bloomshipper.CloseableBlockQuerier) []bloomshipper.BlockRef {
	t.Helper()
	res := make([]bloomshipper.BlockRef, 0)
	for i := range data {
		res = append(res, bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       tenant,
				TableName:      "",
				Bounds:         v1.NewBounds(data[i].Bounds.Min, data[i].Bounds.Max),
				StartTimestamp: 0,
				EndTimestamp:   0,
				Checksum:       0,
			},
		})
	}
	return res
}
