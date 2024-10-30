package v1

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/encoding"
)

func TestBloomOffsetEncoding(t *testing.T) {
	for _, v := range SupportedVersions {
		t.Run(v.String(), func(t *testing.T) {
			src := BloomOffset{Page: 1, ByteOffset: 2}
			enc := &encoding.Encbuf{}
			src.Encode(enc, v, BloomOffset{})

			var dst BloomOffset
			dec := encoding.DecWith(enc.Get())
			require.Nil(t, dst.Decode(&dec, v, BloomOffset{}))

			require.Equal(t, src, dst)
		})
	}

}

func TestSeriesEncoding_V3(t *testing.T) {
	t.Parallel()
	version := V3
	src := SeriesWithMeta{
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
		Meta: Meta{
			Offsets: []BloomOffset{
				{Page: 0, ByteOffset: 0},
				{Page: 0, ByteOffset: 100},
				{Page: 1, ByteOffset: 2},
				{Page: 2, ByteOffset: 1},
			},
			Fields: NewSetFromLiteral[Field]("foo", "bar"),
		},
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc, version, 0, BloomOffset{})

	dec := encoding.DecWith(enc.Get())
	var dst SeriesWithMeta
	fp, offset, err := dst.Decode(&dec, version, 0, BloomOffset{})
	require.Nil(t, err)
	require.Equal(t, src.Fingerprint, fp)
	require.Equal(t, src.Offsets[len(src.Offsets)-1], offset)
	require.Equal(t, src.Offsets, dst.Offsets)
	require.Equal(t, src.Fields, dst.Fields)
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
			expCmp:  -1,
			expLess: true,
		},
		{
			desc:    "From is after",
			left:    ChunkRef{1, 1, 0},
			right:   ChunkRef{0, 1, 0},
			expCmp:  1,
			expLess: false,
		},
		{
			desc:    "Through is before",
			left:    ChunkRef{0, 1, 0},
			right:   ChunkRef{0, 2, 0},
			expCmp:  -1,
			expLess: true,
		},
		{
			desc:    "Through is after",
			left:    ChunkRef{0, 2, 0},
			right:   ChunkRef{0, 1, 0},
			expCmp:  1,
			expLess: false,
		},
		{
			desc:    "Checksum is smaller",
			left:    ChunkRef{0, 1, 0},
			right:   ChunkRef{0, 1, 1},
			expCmp:  -1,
			expLess: true,
		},
		{
			desc:    "Checksum is bigger",
			left:    ChunkRef{0, 0, 1},
			right:   ChunkRef{0, 0, 0},
			expCmp:  1,
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

func TestChunkRefsUnion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc               string
		left, right, union ChunkRefs
	}{
		{
			desc:  "empty",
			left:  nil,
			right: nil,
			union: nil,
		},
		{
			desc:  "left empty",
			left:  nil,
			right: ChunkRefs{{From: 1, Through: 2}},
			union: ChunkRefs{{From: 1, Through: 2}},
		},
		{
			desc:  "right empty",
			left:  ChunkRefs{{From: 1, Through: 2}},
			right: nil,
			union: ChunkRefs{{From: 1, Through: 2}},
		},
		{
			desc:  "left before right",
			left:  ChunkRefs{{From: 1, Through: 2}},
			right: ChunkRefs{{From: 3, Through: 4}},
			union: ChunkRefs{{From: 1, Through: 2}, {From: 3, Through: 4}},
		},
		{
			desc:  "left after right",
			left:  ChunkRefs{{From: 3, Through: 4}},
			right: ChunkRefs{{From: 1, Through: 2}},
			union: ChunkRefs{{From: 1, Through: 2}, {From: 3, Through: 4}},
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
			union: ChunkRefs{
				{From: 1, Through: 3},
				{From: 2, Through: 4},
				{From: 3, Through: 5},
				{From: 4, Through: 6},
				{From: 5, Through: 6},
				{From: 5, Through: 7},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.union, tc.left.Union(tc.right))
		})
	}
}

func TestChunkRefsIntersect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc                   string
		left, right, intersect ChunkRefs
	}{
		{
			desc:      "empty",
			left:      nil,
			right:     nil,
			intersect: nil,
		},
		{
			desc:      "left empty",
			left:      nil,
			right:     ChunkRefs{{From: 1, Through: 2}},
			intersect: nil,
		},
		{
			desc:      "right empty",
			left:      ChunkRefs{{From: 1, Through: 2}},
			right:     nil,
			intersect: nil,
		},
		{
			desc:      "left before right",
			left:      ChunkRefs{{From: 1, Through: 2}},
			right:     ChunkRefs{{From: 3, Through: 4}},
			intersect: nil,
		},
		{
			desc:      "left after right",
			left:      ChunkRefs{{From: 3, Through: 4}},
			right:     ChunkRefs{{From: 1, Through: 2}},
			intersect: nil,
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
			intersect: ChunkRefs{
				{From: 2, Through: 4},
				{From: 4, Through: 6},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.intersect, tc.left.Intersect(tc.right))
		})
	}
}
