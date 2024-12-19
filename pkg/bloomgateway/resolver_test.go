package bloomgateway

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

func makeBlockRef(minFp, maxFp model.Fingerprint, from, through model.Time) bloomshipper.BlockRef {
	return bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			TenantID:       "tenant",
			TableName:      "table",
			Bounds:         v1.NewBounds(minFp, maxFp),
			StartTimestamp: from,
			EndTimestamp:   through,
		},
	}
}

func makeMeta(minFp, maxFp model.Fingerprint, from, through model.Time) bloomshipper.Meta {
	return bloomshipper.Meta{
		Blocks: []bloomshipper.BlockRef{
			makeBlockRef(minFp, maxFp, from, through),
		},
		Sources: []tsdb.SingleTenantTSDBIdentifier{
			{TS: through.Time()},
		},
	}
}

func TestBlockResolver_BlocksMatchingSeries(t *testing.T) {
	interval := bloomshipper.NewInterval(1000, 1999)
	series := []*logproto.GroupedChunkRefs{
		{Fingerprint: 0x00a0},
		{Fingerprint: 0x00b0},
		{Fingerprint: 0x00c0},
		{Fingerprint: 0x00d0},
		{Fingerprint: 0x00e0},
		{Fingerprint: 0x00f0},
	}

	t.Run("no metas/blocks", func(t *testing.T) {
		res := blocksMatchingSeries(nil, interval, series)
		require.Equal(t, []blockWithSeries{}, res)
	})

	t.Run("blocks outside of time interval", func(t *testing.T) {
		metas := []bloomshipper.Meta{
			makeMeta(0x00, 0xff, 0, 999),
			makeMeta(0x00, 0xff, 2000, 2999),
		}
		res := blocksMatchingSeries(metas, interval, series)
		require.Equal(t, []blockWithSeries{}, res)
	})

	t.Run("blocks outside of keyspace", func(t *testing.T) {
		metas := []bloomshipper.Meta{
			makeMeta(0x0000, 0x009f, 1000, 1999),
			makeMeta(0x0100, 0x019f, 1000, 1999),
		}
		res := blocksMatchingSeries(metas, interval, series)
		require.Equal(t, []blockWithSeries{}, res)
	})

	t.Run("single block within time range covering full keyspace", func(t *testing.T) {
		metas := []bloomshipper.Meta{
			makeMeta(0x00, 0xff, 1000, 1999),
		}
		res := blocksMatchingSeries(metas, interval, series)
		expected := []blockWithSeries{
			{
				block:  metas[0].Blocks[0],
				series: series,
			},
		}
		require.Equal(t, expected, res)
	})

	t.Run("multiple blocks within time range covering full keyspace", func(t *testing.T) {
		metas := []bloomshipper.Meta{
			makeMeta(0xa0, 0xcf, 1000, 1999),
			makeMeta(0xd0, 0xff, 1000, 1999),
		}
		res := blocksMatchingSeries(metas, interval, series)
		expected := []blockWithSeries{
			{
				block:  metas[0].Blocks[0],
				series: series[0:3],
			},
			{
				block:  metas[1].Blocks[0],
				series: series[3:6],
			},
		}
		require.Equal(t, expected, res)
	})

	t.Run("multiple overlapping blocks within time range covering full keyspace", func(t *testing.T) {
		metas := []bloomshipper.Meta{
			// 2 series overlap
			makeMeta(0x00, 0xdf, 1000, 1499), // "old" meta covers first 4 series
			makeMeta(0xc0, 0xff, 1500, 1999), // "new" meta covers last 4 series
		}
		res := blocksMatchingSeries(metas, interval, series)
		for i := range res {
			t.Logf("%s", res[i].block)
			for j := range res[i].series {
				t.Logf("  %016x", res[i].series[j].Fingerprint)
			}
		}
		expected := []blockWithSeries{
			{
				block:  metas[0].Blocks[0],
				series: series[0:2], // series 0x00c0 and 0x00d0 are covered in the newer block
			},
			{
				block:  metas[1].Blocks[0],
				series: series[2:6],
			},
		}
		require.Equal(t, expected, res)
	})
}

func TestBlockResolver_UnassignedSeries(t *testing.T) {
	series := []*logproto.GroupedChunkRefs{
		{Fingerprint: 0x00},
		{Fingerprint: 0x20},
		{Fingerprint: 0x40},
		{Fingerprint: 0x60},
		{Fingerprint: 0x80},
		{Fingerprint: 0xa0},
		{Fingerprint: 0xc0},
		{Fingerprint: 0xe0},
	}

	testCases := []struct {
		desc     string
		mapped   []blockWithSeries
		expected []*logproto.GroupedChunkRefs
	}{
		{
			desc:     "no blocks - all unassigned",
			mapped:   []blockWithSeries{},
			expected: series,
		},
		{
			desc: "block has no overlapping series - all unassigned",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0xf0},
						{Fingerprint: 0xff},
					},
				},
			},
			expected: series,
		},
		{
			desc: "single block covering all series - no unassigned",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x00},
						{Fingerprint: 0x20},
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
						{Fingerprint: 0x80},
						{Fingerprint: 0xa0},
						{Fingerprint: 0xc0},
						{Fingerprint: 0xe0},
					},
				},
			},
			expected: []*logproto.GroupedChunkRefs{},
		},
		{
			desc: "multiple blocks covering all series - no unassigned",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x00},
						{Fingerprint: 0x20},
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
					},
				},
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
						{Fingerprint: 0x80},
						{Fingerprint: 0xa0},
					},
				},
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x80},
						{Fingerprint: 0xa0},
						{Fingerprint: 0xc0},
						{Fingerprint: 0xe0},
					},
				},
			},
			expected: []*logproto.GroupedChunkRefs{},
		},
		{
			desc: "single block overlapping some series",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x00},
						{Fingerprint: 0x20},
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
					},
				},
			},
			expected: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x80},
				{Fingerprint: 0xa0},
				{Fingerprint: 0xc0},
				{Fingerprint: 0xe0},
			},
		},
		{
			desc: "multiple blocks overlapping some series",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x20},
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
					},
				},
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x80},
						{Fingerprint: 0xa0},
						{Fingerprint: 0xc0},
					},
				},
			},
			expected: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x00},
				{Fingerprint: 0xe0},
			},
		},
		{
			desc: "block overlapping single remaining series",
			mapped: []blockWithSeries{
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x00},
						{Fingerprint: 0x20},
						{Fingerprint: 0x40},
						{Fingerprint: 0x60},
						{Fingerprint: 0x80},
						{Fingerprint: 0xa0},
						{Fingerprint: 0xc0},
					},
				},
				{
					series: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0xe0},
					},
				},
			},
			expected: []*logproto.GroupedChunkRefs{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := unassignedSeries(tc.mapped, series)
			require.Equal(t, tc.expected, result)
		})
	}
}
