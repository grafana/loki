package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCachedPrefetcher(t *testing.T) {
	t.Run("sets the largest region to the configured max region size", func(t *testing.T) {
		p, err := NewCachedPrefetcher(nil, 8<<20, PrefetcherConfig{MaxRegionBytes: 1 << 20}, nil, nil)

		require.NoError(t, err)
		require.EqualValues(t, 1<<20, p.maxRegionBytes)
	})

	t.Run("accepts a max region size that leaves exactly the entry overhead in the cache item", func(t *testing.T) {
		_, err := NewCachedPrefetcher(nil, 8<<20, PrefetcherConfig{MaxRegionBytes: 8<<20 - cacheItemOverheadBytes}, nil, nil)

		require.NoError(t, err)
	})

	t.Run("returns an error when the max region size and the entry overhead do not fit in the cache item", func(t *testing.T) {
		_, err := NewCachedPrefetcher(nil, 8<<20, PrefetcherConfig{MaxRegionBytes: 8<<20 - cacheItemOverheadBytes + 1}, nil, nil)

		require.EqualError(t, err, "the max region size of the metadata prefetcher, 8323073 bytes, and the entry overhead of 65536 bytes do not fit in a cache item of 8388608 bytes")
	})

	t.Run("accepts the largest max region size that the prefetcher can decode when the cache has no cache item size limit", func(t *testing.T) {
		_, err := NewCachedPrefetcher(nil, 0, PrefetcherConfig{MaxRegionBytes: maxZstdDecodedBytes - cacheItemOverheadBytes}, nil, nil)

		require.NoError(t, err)
	})

	t.Run("returns an error when the max region size is above what the prefetcher can decode and the cache has no cache item size limit", func(t *testing.T) {
		_, err := NewCachedPrefetcher(nil, 0, PrefetcherConfig{MaxRegionBytes: maxZstdDecodedBytes - cacheItemOverheadBytes + 1}, nil, nil)

		require.EqualError(t, err, "the max region size of the metadata prefetcher, 536805377 bytes, and the entry overhead of 65536 bytes do not fit in a cache item of 536870912 bytes")
	})
}

func TestFailureReason(t *testing.T) {
	tests := map[string]struct {
		err  error
		want string
	}{
		"a canceled context is canceled":                               {context.Canceled, reasonCanceled},
		"a deadline is a timeout":                                      {fmt.Errorf("reading the metadata region: %w", context.DeadlineExceeded), reasonTimeout},
		"a failed cache lookup is cache unavailable":                   {fmt.Errorf("%w: %w: %w", ErrNotPrefetchable, errCacheUnavailable, errors.New("cache is down")), reasonCacheUnavailable},
		"a region above the limit is too large":                        {fmt.Errorf("%w: %w: it has at least 2 bytes", ErrNotPrefetchable, errRegionTooLarge), reasonTooLarge},
		"a head without the file metadata is a head too small":         {fmt.Errorf("%w: %w: it has 4 bytes", ErrNotPrefetchable, errHeadTooShort), reasonHeadTooSmall},
		"a column type that this version does not know is unsupported": {fmt.Errorf("%w: section 0: %w", ErrNotPrefetchable, unsupportedError("column 0 has an unknown type")), reasonUnsupported},
		"another not prefetchable error is corrupted":                  {fmt.Errorf("%w: unexpected magic %q", ErrNotPrefetchable, "XXXX"), reasonCorrupted},
		"any other error is a storage error":                           {errors.New("reading the metadata region: connection reset"), reasonStorage},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, failureReason(tc.err))
		})
	}

	t.Run("every reason that the function returns is in the list of reasons", func(t *testing.T) {
		for _, tc := range tests {
			require.Contains(t, failureReasons, failureReason(tc.err))
		}
	})
}

func TestFetchResult_Satisfies(t *testing.T) {
	var (
		fileMetadata = prefetchedRangesID{Kind: kindFileMetadata}
		core         = prefetchedRangesID{Kind: kindCore}
		message      = prefetchedRangesID{Kind: kindMessage}
		traceID      = prefetchedRangesID{Kind: kindColumn, ColumnName: "trace_id"}
		level        = prefetchedRangesID{Kind: kindColumn, ColumnName: "level"}
	)

	tests := map[string]struct {
		found       *fetchResult
		prefetchSet PrefetchSet
		want        bool
	}{
		"is satisfied by the file metadata and the core groups when the prefetch set asks for nothing more": {
			found:       foundGroups(fileMetadata, core),
			prefetchSet: PrefetchSet{},
			want:        true,
		},
		"is not satisfied without the file metadata group": {
			found:       foundGroups(core),
			prefetchSet: PrefetchSet{},
		},
		"is not satisfied without the core group": {
			found:       foundGroups(fileMetadata),
			prefetchSet: PrefetchSet{},
		},
		"is not satisfied by the region alone when the prefetch set does not ask for all metadata": {
			found:       foundRegion(),
			prefetchSet: PrefetchSet{},
		},
		"is satisfied with the message group when the prefetch set asks for the message": {
			found:       foundGroups(fileMetadata, core, message),
			prefetchSet: PrefetchSet{Message: true},
			want:        true,
		},
		"is not satisfied without the message group when the prefetch set asks for the message": {
			found:       foundGroups(fileMetadata, core),
			prefetchSet: PrefetchSet{Message: true},
		},
		"is satisfied without the message group when the prefetch set does not ask for the message": {
			found:       foundGroups(fileMetadata, core),
			prefetchSet: PrefetchSet{Message: false},
			want:        true,
		},
		"is satisfied when every named column has its group": {
			found:       withColumnNames(foundGroups(fileMetadata, core, traceID, level), "trace_id", "level"),
			prefetchSet: PrefetchSet{MetadataNames: []string{"trace_id", "level"}},
			want:        true,
		},
		"is not satisfied when a named column that the object has lacks its group": {
			found:       withColumnNames(foundGroups(fileMetadata, core, traceID), "trace_id", "level"),
			prefetchSet: PrefetchSet{MetadataNames: []string{"trace_id", "level"}},
		},
		"is satisfied when a named column that the object does not have lacks a group": {
			found:       withColumnNames(foundGroups(fileMetadata, core, traceID), "trace_id"),
			prefetchSet: PrefetchSet{MetadataNames: []string{"trace_id", "not_a_column"}},
			want:        true,
		},
		"is satisfied when the core group lists no column, because the object has no column to read": {
			found:       withColumnNames(foundGroups(fileMetadata, core)),
			prefetchSet: PrefetchSet{MetadataNames: []string{"trace_id"}},
			want:        true,
		},
		"is satisfied for all metadata by the region": {
			found:       foundRegion(),
			prefetchSet: PrefetchSet{AllMetadata: true},
			want:        true,
		},
		"is not satisfied for all metadata by the groups alone": {
			found:       withColumnNames(foundGroups(fileMetadata, core, message, traceID, level), "trace_id", "level"),
			prefetchSet: PrefetchSet{AllMetadata: true},
		},
		"is not satisfied for all metadata when the fetch found nothing": {
			found:       newFetchResult(nil),
			prefetchSet: PrefetchSet{AllMetadata: true},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.found.satisfies(tc.prefetchSet))
		})
	}
}

// foundGroups returns a fetch result that found the groups ids. A group holds no range.
func foundGroups(ids ...prefetchedRangesID) *fetchResult {
	f := newFetchResult(ids)
	for _, id := range ids {
		f.ranges[id] = PrefetchedRanges{}
	}
	return f
}

// foundRegion returns a fetch result that found the region and no group.
func foundRegion() *fetchResult {
	f := newFetchResult([]prefetchedRangesID{{Kind: kindRegion}})
	f.region = []byte("region")
	return f
}

// withColumnNames sets the structured metadata columns that the core group of f lists, and returns f.
func withColumnNames(f *fetchResult, names ...string) *fetchResult {
	f.columnNames = map[string]struct{}{}
	for _, name := range names {
		f.columnNames[name] = struct{}{}
	}
	return f
}
