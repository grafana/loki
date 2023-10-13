package v1

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/encoding"
)

// does not include a real bloom offset
func mkBasicSeries(n int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) []SeriesWithOffset {
	var seriesList []SeriesWithOffset
	for i := 0; i < n; i++ {
		var series SeriesWithOffset
		step := (throughFp - fromFp) / (model.Fingerprint(n))
		series.Fingerprint = fromFp + model.Fingerprint(i)*step
		timeDelta := fromTs + (throughTs-fromTs)/model.Time(n)*model.Time(i)
		series.Chunks = []ChunkRef{
			{
				Start:    fromTs + timeDelta*model.Time(i),
				End:      fromTs + timeDelta*model.Time(i),
				Checksum: uint32(i),
			},
		}
		seriesList = append(seriesList, series)
	}
	return seriesList
}

func TestBloomOffsetEncoding(t *testing.T) {
	src := BloomOffset{Page: 1, ByteOffset: 2}
	enc := &encoding.Encbuf{}
	src.Encode(enc, BloomOffset{})

	var dst BloomOffset
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec, BloomOffset{}))

	require.Equal(t, src, dst)
}

func TestSeriesEncoding(t *testing.T) {
	src := SeriesWithOffset{
		Series: Series{
			Fingerprint: model.Fingerprint(1),
			Chunks: []ChunkRef{
				{
					Start:    1,
					End:      2,
					Checksum: 3,
				},
				{
					Start:    4,
					End:      5,
					Checksum: 6,
				},
			},
		},
		Offset: BloomOffset{Page: 2, ByteOffset: 3},
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc, 0, BloomOffset{})

	dec := encoding.DecWith(enc.Get())
	var dst SeriesWithOffset
	fp, offset, err := dst.Decode(&dec, 0, BloomOffset{})
	require.Nil(t, err)
	require.Equal(t, src.Fingerprint, fp)
	require.Equal(t, src.Offset, offset)
	require.Equal(t, src, dst)
}

func TestChunkRefCompare(t *testing.T) {
	for _, tc := range []struct {
		desc                              string
		left, right, exclusive, inclusive ChunkRefs
	}{
		{
			desc:      "empty",
			left:      nil,
			right:     nil,
			exclusive: nil,
			inclusive: nil,
		},
		{
			desc:      "left empty",
			left:      nil,
			right:     ChunkRefs{{Start: 1, End: 2}},
			exclusive: nil,
			inclusive: nil,
		},
		{
			desc:      "right empty",
			left:      ChunkRefs{{Start: 1, End: 2}},
			right:     nil,
			exclusive: ChunkRefs{{Start: 1, End: 2}},
			inclusive: nil,
		},
		{
			desc:      "left before right",
			left:      ChunkRefs{{Start: 1, End: 2}},
			right:     ChunkRefs{{Start: 3, End: 4}},
			exclusive: ChunkRefs{{Start: 1, End: 2}},
			inclusive: nil,
		},
		{
			desc:      "left after right",
			left:      ChunkRefs{{Start: 3, End: 4}},
			right:     ChunkRefs{{Start: 1, End: 2}},
			exclusive: ChunkRefs{{Start: 3, End: 4}},
			inclusive: nil,
		},
		{
			desc: "left overlaps right",
			left: ChunkRefs{
				{Start: 1, End: 3},
				{Start: 2, End: 4},
				{Start: 3, End: 5},
				{Start: 4, End: 6},
				{Start: 5, End: 7},
			},
			right: ChunkRefs{
				{Start: 2, End: 4},
				{Start: 4, End: 6},
				{Start: 5, End: 6}, // not in left
			},
			exclusive: ChunkRefs{
				{Start: 1, End: 3},
				{Start: 3, End: 5},
				{Start: 5, End: 7},
			},
			inclusive: ChunkRefs{
				{Start: 2, End: 4},
				{Start: 4, End: 6},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			exc, inc := tc.left.Compare(tc.right, true)
			require.Equal(t, tc.exclusive, exc, "exclusive cmp")
			require.Equal(t, tc.inclusive, inc, "inclusive cmp")
		})
	}
}
