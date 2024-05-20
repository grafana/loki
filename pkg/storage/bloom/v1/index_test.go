package v1

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/encoding"
)

func TestBloomOffsetEncoding(t *testing.T) {
	t.Parallel()
	src := BloomOffset{Page: 1, ByteOffset: 2}
	enc := &encoding.Encbuf{}
	src.Encode(enc, BloomOffset{})

	var dst BloomOffset
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec, BloomOffset{}))

	require.Equal(t, src, dst)
}

func TestSeriesEncoding(t *testing.T) {
	t.Parallel()
	src := SeriesWithOffset{
		Series: Series{
			Fingerprint: model.Fingerprint(1),
			Chunks: []ChunkRef{
				{
					From:     1,
					Through:  2,
					Checksum: 3,
				},
				{
					From:     4,
					Through:  5,
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

func TestChunkRefCmpLess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc        string
		left, right ChunkRef
		expCmp      int
		expLess     bool
	}{
		{
			desc:    "From/Through/Checksum are equal",
			left:    ChunkRef{0, 0, 0},
			right:   ChunkRef{0, 0, 0},
			expCmp:  0,
			expLess: false,
		},
		{
			desc:    "From is before",
			left:    ChunkRef{0, 1, 0},
			right:   ChunkRef{1, 1, 0},
			expCmp:  1,
			expLess: true,
		},
		{
			desc:    "From is after",
			left:    ChunkRef{1, 1, 0},
			right:   ChunkRef{0, 1, 0},
			expCmp:  -1,
			expLess: false,
		},
		{
			desc:    "Through is before",
			left:    ChunkRef{0, 1, 0},
			right:   ChunkRef{0, 2, 0},
			expCmp:  1,
			expLess: true,
		},
		{
			desc:    "Through is after",
			left:    ChunkRef{0, 2, 0},
			right:   ChunkRef{0, 1, 0},
			expCmp:  -1,
			expLess: false,
		},
		{
			desc:    "Checksum is smaller",
			left:    ChunkRef{0, 1, 0},
			right:   ChunkRef{0, 1, 1},
			expCmp:  1,
			expLess: true,
		},
		{
			desc:    "Checksum is bigger",
			left:    ChunkRef{0, 0, 1},
			right:   ChunkRef{0, 0, 0},
			expCmp:  -1,
			expLess: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.expCmp, tc.left.Cmp(tc.right))
			require.Equal(t, tc.expLess, tc.left.Less(tc.right))
		})
	}
}

func TestChunkRefsCompare(t *testing.T) {
	t.Parallel()
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
			right:     ChunkRefs{{From: 1, Through: 2}},
			exclusive: nil,
			inclusive: nil,
		},
		{
			desc:      "right empty",
			left:      ChunkRefs{{From: 1, Through: 2}},
			right:     nil,
			exclusive: ChunkRefs{{From: 1, Through: 2}},
			inclusive: nil,
		},
		{
			desc:      "left before right",
			left:      ChunkRefs{{From: 1, Through: 2}},
			right:     ChunkRefs{{From: 3, Through: 4}},
			exclusive: ChunkRefs{{From: 1, Through: 2}},
			inclusive: nil,
		},
		{
			desc:      "left after right",
			left:      ChunkRefs{{From: 3, Through: 4}},
			right:     ChunkRefs{{From: 1, Through: 2}},
			exclusive: ChunkRefs{{From: 3, Through: 4}},
			inclusive: nil,
		},
		{
			desc: "left overlaps right",
			left: ChunkRefs{
				{From: 1, Through: 3},
				{From: 2, Through: 4},
				{From: 3, Through: 5},
				{From: 4, Through: 6},
				{From: 5, Through: 7},
			},
			right: ChunkRefs{
				{From: 2, Through: 4},
				{From: 4, Through: 6},
				{From: 5, Through: 6}, // not in left
			},
			exclusive: ChunkRefs{
				{From: 1, Through: 3},
				{From: 3, Through: 5},
				{From: 5, Through: 7},
			},
			inclusive: ChunkRefs{
				{From: 2, Through: 4},
				{From: 4, Through: 6},
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
