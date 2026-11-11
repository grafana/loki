package indexpointers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// optionalFieldPointers returns pointers that cover every combination of set
// and unset optional fields, including unset fields after the last value.
func optionalFieldPointers(t *testing.T) []IndexPointer {
	t.Helper()
	keys, err := postings.NewKeyRange(
		postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"},
		postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api\x00"},
	)
	require.NoError(t, err)
	buckets := NewShardBuckets(streams.MustShardLayoutFromBuckets(64))

	return []IndexPointer{
		{Path: "indexes/a", StartTs: unixTime(10).UTC(), EndTs: unixTime(20).UTC(), RunID: 1, PostingKeys: keys, ShardBuckets: buckets},
		{Path: "indexes/b", StartTs: unixTime(30).UTC(), EndTs: unixTime(40).UTC(), ShardBuckets: buckets},
		{Path: "indexes/c", StartTs: unixTime(50).UTC(), EndTs: unixTime(60).UTC(), RunID: 1, PostingKeys: keys},
		{Path: "indexes/d", StartTs: unixTime(70).UTC(), EndTs: unixTime(80).UTC()},
	}
}

// buildLegacyObject returns an object with an indexpointers section that has
// no optional columns.
func buildLegacyObject(t *testing.T) *dataobj.Object {
	t.Helper()
	legacy := []IndexPointer{{Path: "indexes/a", StartTs: unixTime(10), EndTs: unixTime(20)}}
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(&unknownColumnSectionBuilder{tenant: "tenant-1", pointers: legacy}))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func TestIter(t *testing.T) {
	t.Run("decodes the optional fields of each pointer", func(t *testing.T) {
		pointers := optionalFieldPointers(t)
		obj := buildPointersObject(t, pointers)

		var got []IndexPointer
		for res := range Iter(t.Context(), obj) {
			pointer, err := res.Value()
			require.NoError(t, err)
			got = append(got, utcPointer(pointer.IndexPointer))
		}
		require.Equal(t, pointers, got)
	})

	t.Run("leaves the optional fields unset for a section without optional columns", func(t *testing.T) {
		for res := range Iter(t.Context(), buildLegacyObject(t)) {
			pointer, err := res.Value()
			require.NoError(t, err)
			require.Zero(t, pointer.RunID)
			require.True(t, pointer.PostingKeys.IsZero())
			require.Zero(t, pointer.ShardBuckets)
		}
	})
}

func TestPostingKeysFromBounds(t *testing.T) {
	low := postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"}
	high := postings.Key{Kind: postings.KindLabel, ColumnName: "env", LabelValue: "prod"}
	keys, err := postings.NewKeyRange(low, high)
	require.NoError(t, err)
	lowEncoded, highEncoded := keys.Encode()

	t.Run("returns the range when both bounds are present", func(t *testing.T) {
		keys, err := PostingKeysFromBounds(lowEncoded, highEncoded)
		require.NoError(t, err)
		require.Equal(t, low, keys.Min())
		require.Equal(t, high, keys.Max())
	})

	t.Run("returns no range when both bounds are missing", func(t *testing.T) {
		keys, err := PostingKeysFromBounds(nil, nil)
		require.NoError(t, err)
		require.True(t, keys.IsZero())
	})

	t.Run("returns an error when only the min bound is present", func(t *testing.T) {
		_, err := PostingKeysFromBounds(lowEncoded, nil)
		require.Error(t, err)
	})

	t.Run("returns an error when only the max bound is present", func(t *testing.T) {
		_, err := PostingKeysFromBounds(nil, highEncoded)
		require.Error(t, err)
	})

	t.Run("returns an error when the min bound sorts after the max bound", func(t *testing.T) {
		_, err := PostingKeysFromBounds(highEncoded, lowEncoded)
		require.Error(t, err)
	})

	t.Run("returns an error when a bound is not a valid encoding", func(t *testing.T) {
		_, err := PostingKeysFromBounds([]byte("bad"), highEncoded)
		require.Error(t, err)
	})
}

func TestRunIDFromValue(t *testing.T) {
	t.Run("returns the run ID for a positive value", func(t *testing.T) {
		runID, err := RunIDFromValue(7)
		require.NoError(t, err)
		require.Equal(t, RunID(7), runID)
	})

	t.Run("returns an error when the value is zero", func(t *testing.T) {
		_, err := RunIDFromValue(0)
		require.Error(t, err)
	})

	t.Run("returns an error when the value is negative", func(t *testing.T) {
		_, err := RunIDFromValue(-1)
		require.Error(t, err)
	})
}

func TestShardBucketsFromCount(t *testing.T) {
	t.Run("returns the bucket count for a power of two", func(t *testing.T) {
		buckets, err := ShardBucketsFromCount(64)
		require.NoError(t, err)
		layout, ok := buckets.Layout()
		require.True(t, ok)
		require.Equal(t, uint32(64), layout.Buckets())
	})

	t.Run("returns an error when the count is not a power of two", func(t *testing.T) {
		_, err := ShardBucketsFromCount(3)
		require.Error(t, err)
	})

	t.Run("returns an error when the count is not positive", func(t *testing.T) {
		_, err := ShardBucketsFromCount(0)
		require.Error(t, err)
	})

	t.Run("returns an error when the count does not fit in 32 bits", func(t *testing.T) {
		_, err := ShardBucketsFromCount(1 << 40)
		require.Error(t, err)
	})
}

func TestShardBuckets_Layout(t *testing.T) {
	t.Run("returns no layout for the zero ShardBuckets", func(t *testing.T) {
		_, ok := ShardBuckets{}.Layout()
		require.False(t, ok)
	})

	t.Run("returns the layout that NewShardBuckets got, including one bucket", func(t *testing.T) {
		for _, count := range []uint32{1, 32, 256} {
			want := streams.MustShardLayoutFromBuckets(count)
			got, ok := NewShardBuckets(want).Layout()
			require.True(t, ok)
			require.Equal(t, want, got)
		}
	})
}

func buildPointersObject(t *testing.T, pointers []IndexPointer) *dataobj.Object {
	t.Helper()

	builder := NewBuilder(nil, 0, 2)
	builder.SetTenant("tenant-1")
	for _, pointer := range pointers {
		builder.Append(pointer)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func utcPointer(pointer IndexPointer) IndexPointer {
	pointer.StartTs = pointer.StartTs.UTC()
	pointer.EndTs = pointer.EndTs.UTC()
	return pointer
}
