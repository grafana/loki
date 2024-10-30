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

func TestDaysForRange(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		pairs [2]string
		exp   []string
	}{
		{
			desc:  "single day range",
			pairs: [2]string{"2024-01-24 00:00", "2024-01-24 23:59"},
			exp:   []string{"2024-01-24 00:00"},
		},
		{
			desc:  "two consecutive days",
			pairs: [2]string{"2024-01-24 00:00", "2024-01-25 23:59"},
			exp:   []string{"2024-01-24 00:00", "2024-01-25 00:00"},
		},
		{
			desc:  "multiple days range",
			pairs: [2]string{"2024-01-24 00:00", "2024-01-27 23:59"},
			exp:   []string{"2024-01-24 00:00", "2024-01-25 00:00", "2024-01-26 00:00", "2024-01-27 00:00"},
		},
		{
			desc:  "end of month to beginning of next",
			pairs: [2]string{"2024-01-31 00:00", "2024-02-01 23:59"},
			exp:   []string{"2024-01-31 00:00", "2024-02-01 00:00"},
		},
		{
			desc:  "leap year day range",
			pairs: [2]string{"2024-02-28 00:00", "2024-02-29 23:59"},
			exp:   []string{"2024-02-28 00:00", "2024-02-29 00:00"},
		},
		{
			desc:  "two consecutive days not nicely aligned",
			pairs: [2]string{"2024-01-24 04:00", "2024-01-25 10:00"},
			exp:   []string{"2024-01-24 00:00", "2024-01-25 00:00"},
		},
		{
			desc:  "two consecutive days end zeroed trimmed",
			pairs: [2]string{"2024-01-24 00:00", "2024-01-25 00:00"},
			exp:   []string{"2024-01-24 00:00"},
		},
		{
			desc:  "preserve one day",
			pairs: [2]string{"2024-01-24 00:00", "2024-01-24 00:00"},
			exp:   []string{"2024-01-24 00:00"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			from := mktime(tc.pairs[0])
			through := mktime(tc.pairs[1])
			result := daysForRange(from, through)
			var collected []string
			for _, d := range result {
				parsed := d.Time().UTC().Format("2006-01-02 15:04")
				collected = append(collected, parsed)
			}

			require.Equal(t, tc.exp, collected)
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

func TestPartitionTasksByBlock(t *testing.T) {

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

		results := partitionTasksByBlock(tasks, bounds)
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

		results := partitionTasksByBlock([]Task{task}, bounds)
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

		results := partitionTasksByBlock(tasks, bounds)
		require.Len(t, results, 0)
	})

	t.Run("overlapping and unsorted block ranges", func(t *testing.T) {
		bounds := []bloomshipper.BlockRef{
			mkBlockRef(5, 14),
			mkBlockRef(0, 9),
			mkBlockRef(10, 19),
		}

		tasks := []Task{
			{
				series: []*logproto.GroupedChunkRefs{
					{Fingerprint: 6},
				},
			},
			{
				series: []*logproto.GroupedChunkRefs{
					{Fingerprint: 12},
				},
			},
		}

		expected := []blockWithTasks{
			{ref: bounds[0], tasks: tasks},     // both tasks
			{ref: bounds[1], tasks: tasks[:1]}, // first task
			{ref: bounds[2], tasks: tasks[1:]}, // second task
		}
		results := partitionTasksByBlock(tasks, bounds)
		require.Equal(t, expected, results)
	})
}

func TestPartitionRequest(t *testing.T) {
	ts := mktime("2024-01-24 12:00")

	testCases := map[string]struct {
		inp *logproto.FilterChunkRefRequest
		exp []seriesWithInterval
	}{

		"no series": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-12 * time.Hour),
				Through: ts.Add(12 * time.Hour),
				Refs:    []*logproto.GroupedChunkRefs{},
			},
			exp: []seriesWithInterval{},
		},

		"no chunks for series": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-12 * time.Hour),
				Through: ts.Add(12 * time.Hour),
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs:        []*logproto.ShortRef{},
					},
					{
						Fingerprint: 0x10,
						Refs:        []*logproto.ShortRef{},
					},
				},
			},
			exp: []seriesWithInterval{},
		},

		"chunks before and after requested day": {
			inp: &logproto.FilterChunkRefRequest{
				From:    ts.Add(-2 * time.Hour),
				Through: ts.Add(2 * time.Hour),
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-14 * time.Hour), Through: ts.Add(-13 * time.Hour)},
							{From: ts.Add(13 * time.Hour), Through: ts.Add(14 * time.Hour)},
						},
					},
				},
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
							{From: ts.Add(-14 * time.Hour), Through: ts.Add(-13 * time.Hour)}, // previous day
							{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)}, // previous & target day
							{From: ts.Add(-11 * time.Hour), Through: ts.Add(-10 * time.Hour)}, // target day
						},
					},
				},
			},
			exp: []seriesWithInterval{
				// previous day
				{
					interval: bloomshipper.Interval{Start: ts.Add(-14 * time.Hour), End: ts.Add(-12 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-23 00:00")),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-14 * time.Hour), Through: ts.Add(-13 * time.Hour)}, // previous day
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)}, // previous & target day
							},
						},
					},
				},
				// target day
				{
					interval: bloomshipper.Interval{Start: ts.Add(-12 * time.Hour), End: ts.Add(-10 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-24 00:00")),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-11 * time.Hour)}, // previous & target day
								{From: ts.Add(-11 * time.Hour), Through: ts.Add(-10 * time.Hour)}, // target day
							},
						},
					},
				},
			},
		},

		"through target day inclusion": {
			inp: &logproto.FilterChunkRefRequest{
				// Only search for the target day, but ensure chunks whose through (but not from)
				// is on the target day are included
				From:    ts.Add(-1 * time.Hour),
				Through: ts,
				Refs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: 0x00,
						Refs: []*logproto.ShortRef{
							{From: ts.Add(-13 * time.Hour), Through: ts.Add(-1 * time.Hour)}, // previous & target day
						},
					},
				},
			},
			exp: []seriesWithInterval{
				{
					interval: bloomshipper.Interval{Start: ts.Add(-12 * time.Hour), End: ts.Add(-1 * time.Hour)},
					day:      config.NewDayTime(mktime("2024-01-24 00:00")),
					series: []*logproto.GroupedChunkRefs{
						{
							Fingerprint: 0x00,
							Refs: []*logproto.ShortRef{
								{From: ts.Add(-13 * time.Hour), Through: ts.Add(-1 * time.Hour)}, // inherited from the chunk
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

			// Run each partition through the same partitioning process again to make sure results are still the same
			for i := range result {
				result2 := partitionSeriesByDay(result[i].interval.Start, result[i].interval.End, result[i].series)
				require.Len(t, result2, 1)
				require.Equal(t, result[i], result2[0])
			}
		})
	}
}

func createBlocks(t *testing.T, tenant string, n int, from, through model.Time, minFp, maxFp model.Fingerprint) ([]bloomshipper.BlockRef, []bloomshipper.Meta, []*v1.Block, [][]v1.SeriesWithBlooms) {
	t.Helper()

	blockRefs := make([]bloomshipper.BlockRef, 0, n)
	metas := make([]bloomshipper.Meta, 0, n)
	blocks := make([]*v1.Block, 0, n)
	series := make([][]v1.SeriesWithBlooms, 0, n)

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

		blocks = append(blocks, block)
		metas = append(metas, meta)
		blockRefs = append(blockRefs, blockRef)
		series = append(series, data)
	}
	return blockRefs, metas, blocks, series
}

func createQueryInputFromBlockData(t *testing.T, tenant string, data [][]v1.SeriesWithBlooms, nthSeries int) []*logproto.ChunkRef {
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

func TestOverlappingChunks(t *testing.T) {
	mkRef := func(from, through model.Time) *logproto.ShortRef {
		return &logproto.ShortRef{From: from, Through: through}
	}

	for _, tc := range []struct {
		desc           string
		from, through  model.Time
		input          []*logproto.ShortRef
		exp            []*logproto.ShortRef
		expMin, expMax model.Time
	}{
		{
			desc: "simple ordered",
			from: 0, through: 10,
			input: []*logproto.ShortRef{
				mkRef(0, 2),
				mkRef(3, 5),
				mkRef(6, 8),
				mkRef(10, 12),
				mkRef(14, 16),
			},
			exp: []*logproto.ShortRef{
				mkRef(0, 2),
				mkRef(3, 5),
				mkRef(6, 8),
				mkRef(10, 12),
			},
			expMin: 0, expMax: 10,
		},
		{
			desc: "refs through timestamps aren't in monotonic order",
			from: 0, through: 10,
			input: []*logproto.ShortRef{
				mkRef(0, 2),
				mkRef(3, 5),
				mkRef(6, 8),
				mkRef(10, 12),
				mkRef(14, 16),
			},
			exp: []*logproto.ShortRef{
				mkRef(0, 2),
				mkRef(3, 5),
				mkRef(6, 8),
				mkRef(10, 12),
			},
			expMin: 0, expMax: 10,
		},
		{
			desc: "expMin & expMax are within from/through",
			from: 10, through: 20,
			input: []*logproto.ShortRef{
				mkRef(0, 2),
				mkRef(3, 5),
				mkRef(6, 8),
				mkRef(14, 16),
				mkRef(17, 19),
				mkRef(21, 30),
			},
			exp: []*logproto.ShortRef{
				mkRef(14, 16),
				mkRef(17, 19),
			},
			expMin: 14, expMax: 19,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			minTs, maxTs, got := overlappingChunks(tc.from, tc.through, model.Latest, model.Earliest, tc.input)
			require.Equal(t, tc.expMin, minTs)
			require.Equal(t, tc.expMax, maxTs)
			require.Equal(t, tc.exp, got)
		})
	}
}
