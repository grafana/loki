package v1

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/util/encoding"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestBloomOffsetEncoding(t *testing.T) {
	src := BloomOffset{PageOffset: 1, ByteOffset: 2}
	enc := &encoding.Encbuf{}
	src.Encode(enc)

	var dst BloomOffset
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec))

	require.Equal(t, src, dst)
}

func TestSeriesEncoding(t *testing.T) {
	src := Series{
		Fingerprint: model.Fingerprint(1),
		Offset:      BloomOffset{PageOffset: 2, ByteOffset: 3},
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
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc, 0)

	dec := encoding.DecWith(enc.Get())
	var dst Series
	fp, err := dst.Decode(&dec, 0)
	require.Nil(t, err)
	require.Equal(t, src.Fingerprint, fp)
	require.Equal(t, src, dst)
}

func TestSeriesPageEncoding(t *testing.T) {
	series := []Series{
		{
			Fingerprint: model.Fingerprint(1),
			Offset:      BloomOffset{PageOffset: 2, ByteOffset: 3},
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
		{
			Fingerprint: model.Fingerprint(2),
			Offset:      BloomOffset{PageOffset: 2, ByteOffset: 3},
			Chunks: []ChunkRef{
				{
					Start:    7,
					End:      8,
					Checksum: 9,
				},
				{
					Start:    10,
					End:      11,
					Checksum: 12,
				},
			},
		},
	}

	src := SeriesPage{
		Header: SeriesHeader{
			NumSeries: len(series),
			FromFp:    series[0].Fingerprint,
			ThroughFp: series[len(series)-1].Fingerprint,
			FromTs:    1,
			ThroughTs: 11,
		},
		Series: series,
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc)

	var dst SeriesPage
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec))
}

func TestBloomPageEncoding(t *testing.T) {
	var blooms []Bloom
	n := 2
	for i := 0; i < n; i++ {
		var bloom Bloom
		bloom.sbf = *boom.NewScalableBloomFilter(1024, 0.01, 0.8)
		bloom.sbf.Add([]byte(fmt.Sprint(i)))
		blooms = append(blooms, bloom)
	}

	src := BloomPage{
		N:      n,
		Blooms: blooms,
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc)

	var dst BloomPage
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec))
	require.Equal(t, src, dst)
}
