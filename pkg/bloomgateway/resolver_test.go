package bloomgateway

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func makeMeta(minFp, maxFp model.Fingerprint, from, through model.Time) bloomshipper.Meta {
	return bloomshipper.Meta{
		Blocks: []bloomshipper.BlockRef{
			{
				Ref: bloomshipper.Ref{
					TenantID:       "tenant",
					TableName:      "table",
					Bounds:         v1.NewBounds(minFp, maxFp),
					StartTimestamp: from,
					EndTimestamp:   through,
				},
			},
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
			makeMeta(0x00, 0xdf, 1000, 1999),
			makeMeta(0xc0, 0xff, 1000, 1999),
		}
		res := blocksMatchingSeries(metas, interval, series)
		expected := []blockWithSeries{
			{
				block:  metas[0].Blocks[0],
				series: series[0:4],
			},
			{
				block:  metas[1].Blocks[0],
				series: series[2:6],
			},
		}
		require.Equal(t, expected, res)
	})
}
