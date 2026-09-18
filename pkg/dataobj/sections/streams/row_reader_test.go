package streams_test

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	sec := buildStreamsSection(t, 1, 0) // Many pages
	r := streams.NewRowReader(sec)
	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_AddLabelMatcher(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
	}

	sec := buildStreamsSection(t, 1, 0) // Many pages
	r := streams.NewRowReader(sec)
	require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))

	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_ShardBucketRange(t *testing.T) {
	// The reader must return exactly the streams whose bucket falls in [From, To], across single-bucket,
	// multi-bucket, boundary-trimmed, and empty ranges. This checks row filtering; it does not exercise
	// page pruning, since buildStreamsSection is not sorted by shard bucket.
	all := []streams.Stream{
		{1, unixTime(10), unixTime(15), 25, labels.FromStrings("cluster", "test", "app", "foo"), 2, shardForApp("foo")},
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2, shardForApp("baz")},
	}
	fb, bb, zb := uint64(shardForApp("foo")), uint64(shardForApp("bar")), uint64(shardForApp("baz"))
	lo, hi := min(fb, bb, zb), max(fb, bb, zb)

	// A bucket no stream occupies, for the empty-range case.
	occupied := map[uint64]bool{fb: true, bb: true, zb: true}
	var emptyBucket uint64
	for b := uint64(0); ; b++ {
		if !occupied[b] {
			emptyBucket = b
			break
		}
	}

	cases := []struct {
		name     string
		from, to uint64
	}{
		{"single bucket", bb, bb},
		{"span covering every bucket", lo, hi},
		{"span trimmed at the top end", lo, hi - 1},
		{"empty range matches nothing", emptyBucket, emptyBucket},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var want []streams.Stream
			for _, s := range all {
				if uint64(s.ShardBucket) >= tc.from && uint64(s.ShardBucket) <= tc.to {
					want = append(want, s)
				}
			}

			sec := buildStreamsSection(t, 1, 0) // Many pages
			r := streams.NewRowReader(sec)
			require.NoError(t, r.SetPredicate(streams.ShardBucketRangeRowPredicate{From: tc.from, To: tc.to}))

			actual, err := readAllStreams(context.Background(), r)
			require.NoError(t, err)
			if len(want) == 0 {
				require.Empty(t, actual)
			} else {
				require.Equal(t, want, actual)
			}
		})
	}
}

func TestRowReader_AddLabelFilter(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2, shardForApp("bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2, shardForApp("baz")},
	}

	sec := buildStreamsSection(t, 1, 0) // Many pages
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

func TestRowReader_ReadBeforeOpen(t *testing.T) {
	sec := buildStreamsSection(t, 1, 0)
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

func buildStreamsSection(t *testing.T, pageSize, pageRows int) *streams.Section {
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
