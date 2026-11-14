package metadata

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefetchedRanges_Valid(t *testing.T) {
	tests := []struct {
		name   string
		ranges PrefetchedRanges
		want   bool
	}{
		{
			name: "no ranges are valid",
			want: true,
		},
		{
			name: "sorted ranges with a gap between them are valid",
			ranges: PrefetchedRanges{
				{Offset: 0, Data: []byte("abcd")},
				{Offset: 10, Data: []byte("ef")},
			},
			want: true,
		},
		{
			name: "adjacent ranges are valid",
			ranges: PrefetchedRanges{
				{Offset: 0, Data: []byte("abcd")},
				{Offset: 4, Data: []byte("ef")},
			},
			want: true,
		},
		{
			name: "a range that starts inside the previous range is invalid",
			ranges: PrefetchedRanges{
				{Offset: 0, Data: []byte("abcd")},
				{Offset: 3, Data: []byte("ef")},
			},
		},
		{
			name: "ranges out of order are invalid",
			ranges: PrefetchedRanges{
				{Offset: 10, Data: []byte("ef")},
				{Offset: 0, Data: []byte("abcd")},
			},
		},
		{
			name: "a negative offset is invalid",
			ranges: PrefetchedRanges{
				{Offset: -1, Data: []byte("abcd")},
			},
		},
		{
			name: "an end that overflows int64 is invalid",
			ranges: PrefetchedRanges{
				{Offset: math.MaxInt64 - 1, Data: []byte("abcd")},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.ranges.Valid())
		})
	}
}

func TestPrefetchedRanges_mergeAdjacent(t *testing.T) {
	region := []byte("0123456789abcdefghij")
	rangeOf := func(offset, length int64) PrefetchedRange {
		return PrefetchedRange{Offset: offset, Data: region[offset : offset+length : offset+length]}
	}

	tests := map[string]struct {
		ranges PrefetchedRanges
		want   PrefetchedRanges
	}{
		"joins two ranges that touch each other": {
			ranges: PrefetchedRanges{rangeOf(2, 2), rangeOf(4, 2)},
			want:   PrefetchedRanges{rangeOf(2, 4)},
		},
		"joins a chain of ranges that touch each other": {
			ranges: PrefetchedRanges{rangeOf(0, 2), rangeOf(2, 2), rangeOf(4, 2)},
			want:   PrefetchedRanges{rangeOf(0, 6)},
		},
		"keeps ranges that have a gap between them": {
			ranges: PrefetchedRanges{rangeOf(2, 2), rangeOf(5, 2)},
			want:   PrefetchedRanges{rangeOf(2, 2), rangeOf(5, 2)},
		},
		"joins the ranges that touch and keeps the ones that are apart": {
			ranges: PrefetchedRanges{rangeOf(0, 2), rangeOf(2, 2), rangeOf(8, 2)},
			want:   PrefetchedRanges{rangeOf(0, 4), rangeOf(8, 2)},
		},
		"keeps ranges that overlap": {
			ranges: PrefetchedRanges{rangeOf(0, 4), rangeOf(2, 4)},
			want:   PrefetchedRanges{rangeOf(0, 4), rangeOf(2, 4)},
		},
		"returns no ranges for no ranges": {},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := append(PrefetchedRanges(nil), tc.ranges...).mergeAdjacent(region)

			require.Equal(t, tc.want, got)
			for _, r := range got {
				require.Equal(t, len(r.Data), cap(r.Data), "a joined range must not grow into its neighbour")
				require.Same(t, &region[r.Offset], &r.Data[0], "a joined range must alias the region")
			}
		})
	}
}

func TestPrefetchedRangesID_cacheKey(t *testing.T) {
	t.Run("the hashed key has the same length for a short and a long path, within the memcached limit", func(t *testing.T) {
		const memcachedMaxKeyBytes = 250
		id := prefetchedRangesID{Kind: kindColumn, ColumnName: "trace_id"}
		longPath := strings.Repeat("p", 400)

		_, short := id.cacheKey("objects/ab/cd")
		_, long := id.cacheKey(longPath)

		require.Len(t, long, len(short))
		require.LessOrEqual(t, len(long), memcachedMaxKeyBytes)
		require.NotContains(t, long, longPath)
		require.NotContains(t, short, "objects/ab/cd")
	})

	t.Run("the unhashed key holds the whole path and the whole column name", func(t *testing.T) {
		path := strings.Repeat("p", 400)

		unhashed, _ := prefetchedRangesID{Kind: kindColumn, ColumnName: "trace_id"}.cacheKey(path)

		require.Contains(t, unhashed, path)
		require.Contains(t, unhashed, "trace_id")
	})

	t.Run("the groups of one object have distinct unhashed and hashed keys", func(t *testing.T) {
		ids := []prefetchedRangesID{
			{Kind: kindFileMetadata},
			{Kind: kindCore},
			{Kind: kindMessage},
			{Kind: kindRegion},
			{Kind: kindColumn, ColumnName: "trace_id"},
			{Kind: kindColumn, ColumnName: "level"},
		}

		unhashedKeys, hashedKeys := map[string]struct{}{}, map[string]struct{}{}
		for _, id := range ids {
			unhashed, hashed := id.cacheKey("objects/ab/cd")
			unhashedKeys[unhashed], hashedKeys[hashed] = struct{}{}, struct{}{}
		}
		require.Len(t, unhashedKeys, len(ids))
		require.Len(t, hashedKeys, len(ids))
	})

	t.Run("groups of different objects have distinct unhashed keys when a name holds the separator", func(t *testing.T) {
		columnKey, _ := prefetchedRangesID{Kind: kindColumn, ColumnName: "b#core"}.cacheKey("a")
		coreKey, _ := prefetchedRangesID{Kind: kindCore}.cacheKey("a#col:b")

		require.NotEqual(t, columnKey, coreKey)
	})
}
