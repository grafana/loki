package bloomgateway

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
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

func createBlockQueriers(t *testing.T, numBlocks int, from, through model.Time, minFp, maxFp model.Fingerprint) ([]bloomshipper.BlockQuerierWithFingerprintRange, [][]v1.SeriesWithBloom) {
	t.Helper()
	step := (maxFp - minFp) / model.Fingerprint(numBlocks)
	bqs := make([]bloomshipper.BlockQuerierWithFingerprintRange, 0, numBlocks)
	series := make([][]v1.SeriesWithBloom, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		fromFp := minFp + (step * model.Fingerprint(i))
		throughFp := fromFp + step - 1
		// last block needs to include maxFp
		if i == numBlocks-1 {
			throughFp = maxFp
		}
		blockQuerier, data := v1.MakeBlockQuerier(t, fromFp, throughFp, from, through)
		bq := bloomshipper.BlockQuerierWithFingerprintRange{
			BlockQuerier:      blockQuerier,
			FingerprintBounds: v1.NewBounds(fromFp, throughFp),
		}
		bqs = append(bqs, bq)
		series = append(series, data)
	}
	return bqs, series
}

func createBlocks(t *testing.T, tenant string, n int, from, through model.Time, minFp, maxFp model.Fingerprint) ([]bloomshipper.BlockRef, []bloomshipper.Meta, []bloomshipper.BlockQuerierWithFingerprintRange, [][]v1.SeriesWithBloom) {
	t.Helper()

	blocks := make([]bloomshipper.BlockRef, 0, n)
	metas := make([]bloomshipper.Meta, 0, n)
	queriers := make([]bloomshipper.BlockQuerierWithFingerprintRange, 0, n)
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
			TableName:      "table_0",
			MinFingerprint: uint64(fromFp),
			MaxFingerprint: uint64(throughFp),
			StartTimestamp: from,
			EndTimestamp:   through,
		}
		block := bloomshipper.BlockRef{
			Ref:       ref,
			IndexPath: "index.tsdb.gz",
			BlockPath: fmt.Sprintf("block-%d", i),
		}
		meta := bloomshipper.Meta{
			MetaRef: bloomshipper.MetaRef{
				Ref: ref,
			},
			Tombstones: []bloomshipper.BlockRef{},
			Blocks:     []bloomshipper.BlockRef{block},
		}
		blockQuerier, data := v1.MakeBlockQuerier(t, fromFp, throughFp, from, through)
		querier := bloomshipper.BlockQuerierWithFingerprintRange{
			BlockQuerier:      blockQuerier,
			FingerprintBounds: v1.NewBounds(fromFp, throughFp),
		}
		queriers = append(queriers, querier)
		metas = append(metas, meta)
		blocks = append(blocks, block)
		series = append(series, data)
	}
	return blocks, metas, queriers, series
}

func newMockBloomStore(bqs []bloomshipper.BlockQuerierWithFingerprintRange) *mockBloomStore {
	return &mockBloomStore{bqs: bqs}
}

type mockBloomStore struct {
	bqs []bloomshipper.BlockQuerierWithFingerprintRange
	// mock how long it takes to serve block queriers
	delay time.Duration
	// mock response error when serving block queriers in ForEach
	err error
}

var _ bloomshipper.Interface = &mockBloomStore{}

// GetBlockRefs implements bloomshipper.Interface
func (s *mockBloomStore) GetBlockRefs(_ context.Context, tenant string, _ bloomshipper.Interval) ([]bloomshipper.BlockRef, error) {
	time.Sleep(s.delay)
	blocks := make([]bloomshipper.BlockRef, 0, len(s.bqs))
	for i := range s.bqs {
		blocks = append(blocks, bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				MinFingerprint: uint64(s.bqs[i].Min),
				MaxFingerprint: uint64(s.bqs[i].Max),
				TenantID:       tenant,
			},
		})
	}
	return blocks, nil
}

// Stop implements bloomshipper.Interface
func (s *mockBloomStore) Stop() {}

// Fetch implements bloomshipper.Interface
func (s *mockBloomStore) Fetch(_ context.Context, _ string, _ []bloomshipper.BlockRef, callback bloomshipper.ForEachBlockCallback) error {
	if s.err != nil {
		time.Sleep(s.delay)
		return s.err
	}

	shuffled := make([]bloomshipper.BlockQuerierWithFingerprintRange, len(s.bqs))
	_ = copy(shuffled, s.bqs)

	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, bq := range shuffled {
		// ignore errors in the mock
		time.Sleep(s.delay)
		err := callback(bq.BlockQuerier, bq.FingerprintBounds)
		if err != nil {
			return err
		}
	}
	return nil
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
					From:        chk.Start,
					Through:     chk.End,
					Checksum:    chk.Checksum,
				})
			}
			n++
		}
	}
	return res
}

func createBlockRefsFromBlockData(t *testing.T, tenant string, data []bloomshipper.BlockQuerierWithFingerprintRange) []bloomshipper.BlockRef {
	t.Helper()
	res := make([]bloomshipper.BlockRef, 0)
	for i := range data {
		res = append(res, bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       tenant,
				TableName:      "",
				MinFingerprint: uint64(data[i].Min),
				MaxFingerprint: uint64(data[i].Max),
				StartTimestamp: 0,
				EndTimestamp:   0,
				Checksum:       0,
			},
			IndexPath: fmt.Sprintf("index-%d", i),
			BlockPath: fmt.Sprintf("block-%d", i),
		})
	}
	return res
}
