package streams_test

import (
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

var streamsTestdata = []struct {
	Labels           labels.Labels
	Timestamp        time.Time
	UncompressedSize int64
}{
	{labels.FromStrings("cluster", "test", "app", "foo"), unixTime(10), 15},
	{labels.FromStrings("cluster", "test", "app", "foo"), unixTime(15), 10},
	{labels.FromStrings("cluster", "test", "app", "bar"), unixTime(5), 20},
	{labels.FromStrings("cluster", "test", "app", "bar"), unixTime(20), 25},
	{labels.FromStrings("cluster", "test", "app", "baz"), unixTime(25), 30},
	{labels.FromStrings("cluster", "test", "app", "baz"), unixTime(30), 5},
}

func shardForApp(app string) int64 {
	return int64(streams.ShardBucket(labels.FromStrings("cluster", "test", "app", app)))
}

func TestRowReader(t *testing.T) {
	expect := []streams.Stream{
		{1, unixTime(10), unixTime(15), 25, labels.FromStrings("cluster", "test", "app", "foo"), 2, shardForApp("foo")},
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2, shardForApp("baz")},
	}

	sec := buildTestStreamsSection(t, 1, 0) // Many pages
	r := streams.NewRowReader(sec)
	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_AddLabelMatcher(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
	}

	sec := buildTestStreamsSection(t, 1, 0) // Many pages
	r := streams.NewRowReader(sec)
	require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))

	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_AddLabelFilter(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2, shardForApp("baz")},
	}

	sec := buildTestStreamsSection(t, 1, 0) // Many pages
	r := streams.NewRowReader(sec)
	err := r.SetPredicate(streams.LabelFilterRowPredicate{
		Name: "app",
		Keep: func(name, value string) bool {
			require.Equal(t, "app", name)
			return strings.HasPrefix(value, "b")
		},
	})
	require.NoError(t, err)

	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_ShardBucketRange(t *testing.T) {
	all := []streams.Stream{
		{1, unixTime(10), unixTime(15), 25, labels.FromStrings("cluster", "test", "app", "foo"), 2, shardForApp("foo")},
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2, shardForApp("baz")},
	}

	buckets := make(map[uint64]bool, len(all))
	lo, hi := uint64(math.MaxUint64), uint64(0)
	for _, s := range all {
		b := uint64(s.ShardBucket)
		buckets[b] = true
		lo, hi = min(lo, b), max(hi, b)
	}
	require.Greater(t, hi, lo, "the test data must span at least two buckets")

	// A bucket none of the streams occupies, for the empty-range case.
	var freeBucket uint64
	for b := uint64(0); ; b++ {
		if !buckets[b] {
			freeBucket = b
			break
		}
	}

	for _, tc := range []struct {
		name     string
		from, to uint64
	}{
		{"a single occupied bucket", uint64(all[1].ShardBucket), uint64(all[1].ShardBucket)},
		{"a span covering every bucket", lo, hi},
		{"a span trimmed at the top end", lo, hi - 1},
		{"a span trimmed at the bottom end", lo + 1, hi},
		{"a range no stream occupies", freeBucket, freeBucket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want []streams.Stream
			for _, s := range all {
				if b := uint64(s.ShardBucket); b >= tc.from && b <= tc.to {
					want = append(want, s)
				}
			}

			r := streams.NewRowReader(buildTestStreamsSection(t, 1, 0)) // Many pages
			require.NoError(t, r.SetPredicate(streams.ShardBucketRangeRowPredicate{From: tc.from, To: tc.to}))

			got, err := readAllStreams(context.Background(), r)
			require.NoError(t, err)
			if len(want) == 0 {
				require.Empty(t, got)
				return
			}
			require.Equal(t, want, got)
		})
	}
}

func TestRowReader_MatchStreams(t *testing.T) {
	// streamsTestdata maps app=foo to stream ID 1, app=bar to 2 and app=baz to 3.
	idsOf := func(got []streams.Stream) []int64 {
		ids := make([]int64, 0, len(got))
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		return ids
	}

	read := func(t *testing.T, setup func(r *streams.RowReader)) []int64 {
		t.Helper()

		r := streams.NewRowReader(buildTestStreamsSection(t, 1, 0)) // Many pages
		setup(r)
		got, err := readAllStreams(context.Background(), r)
		require.NoError(t, err)
		return idsOf(got)
	}

	t.Run("a subset of the stream IDs", func(t *testing.T) {
		ids := read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1, 3})))
		})
		require.Equal(t, []int64{1, 3}, ids)
	})

	t.Run("repeated calls union the sets", func(t *testing.T) {
		ids := read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{3})))
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
		})
		require.Equal(t, []int64{1, 3}, ids)
	})

	t.Run("an empty set applies no filter", func(t *testing.T) {
		ids := read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{})))
		})
		require.Equal(t, []int64{1, 2, 3}, ids)
	})

	t.Run("an unknown ID matches nothing", func(t *testing.T) {
		ids := read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{99})))
		})
		require.Empty(t, ids)
	})

	t.Run("the match is ANDed with the predicate", func(t *testing.T) {
		// Stream 1 is app=foo, so the label matcher leaves nothing.
		ids := read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
			require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))
		})
		require.Empty(t, ids)

		ids = read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1, 2})))
			require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))
		})
		require.Equal(t, []int64{2}, ids)
	})

	t.Run("Reset clears the matched streams", func(t *testing.T) {
		sec := buildTestStreamsSection(t, 1, 0)
		r := streams.NewRowReader(sec)
		require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
		r.Reset(sec)

		got, err := readAllStreams(context.Background(), r)
		require.NoError(t, err)
		require.Equal(t, []int64{1, 2, 3}, idsOf(got))
	})

	t.Run("the matched streams cannot be changed once reading has started", func(t *testing.T) {
		r := streams.NewRowReader(buildTestStreamsSection(t, 1, 0))
		require.NoError(t, r.Open(context.Background()))
		require.ErrorContains(t, r.MatchStreams(slices.Values([]int64{1})), "cannot change matched streams")
	})
}

func TestRowReader_ReadBeforeOpen(t *testing.T) {
	sec := buildTestStreamsSection(t, 1, 0)
	r := streams.NewRowReader(sec)

	buf := make([]streams.Stream, 1)
	n, err := r.Read(context.Background(), buf)
	require.Zero(t, n)
	require.ErrorContains(t, err, "row reader not opened")
}

func TestRowReader_OpenNilSection(t *testing.T) {
	r := streams.NewRowReader(nil)
	require.NoError(t, r.Open(context.Background()))

	buf := make([]streams.Stream, 1)
	n, err := r.Read(context.Background(), buf)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

func buildTestStreamsSection(t *testing.T, pageSize, pageRows int) *streams.Section {
	t.Helper()

	s := streams.NewBuilder(nil, pageSize, pageRows)
	for _, d := range streamsTestdata {
		s.Record(d.Labels, d.Timestamp, d.UncompressedSize)
	}

	builder := dataobj.NewBuilder(nil)
	require.NoError(t, builder.Append(s))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := streams.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllStreams(ctx context.Context, r *streams.RowReader) ([]streams.Stream, error) {
	var (
		res []streams.Stream
		buf = make([]streams.Stream, 128)
	)
	if err := r.Open(ctx); err != nil {
		return nil, err
	}

	for {
		n, err := r.Read(ctx, buf)
		if n > 0 {
			res = append(res, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			return res, nil
		} else if err != nil {
			return res, err
		}

		clear(buf)
	}
}
