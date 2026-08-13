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

func TestRowReader_MatchStreams(t *testing.T) {
	// streamsTestdata has three streams: app=foo → ID 1, app=bar → ID 2, app=baz → ID 3.
	sec := buildStreamsSection(t, 1, 0) // Many pages.

	read := func(t *testing.T, setup func(r *streams.RowReader)) []int64 {
		t.Helper()
		r := streams.NewRowReader(sec)
		setup(r)
		got, err := readAllStreams(context.Background(), r)
		require.NoError(t, err)
		return idsOf(got)
	}
	match := func(ids ...int64) func(*streams.RowReader) {
		return func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values(ids)))
		}
	}

	t.Run("subset", func(t *testing.T) {
		require.ElementsMatch(t, []int64{1, 3}, read(t, match(1, 3)))
	})
	t.Run("all", func(t *testing.T) {
		require.ElementsMatch(t, []int64{1, 2, 3}, read(t, match(1, 2, 3)))
	})
	t.Run("empty set is no filter", func(t *testing.T) {
		require.ElementsMatch(t, []int64{1, 2, 3}, read(t, match()))
	})
	t.Run("no match returns nothing", func(t *testing.T) {
		require.Empty(t, read(t, match(999)))
	})
	t.Run("cumulative across calls", func(t *testing.T) {
		require.ElementsMatch(t, []int64{1, 3}, read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
			require.NoError(t, r.MatchStreams(slices.Values([]int64{3})))
		}))
	})
	t.Run("ANDed with a label predicate", func(t *testing.T) {
		// app=bar is ID 2, so the intersection with {2,3} is {2}.
		require.ElementsMatch(t, []int64{2}, read(t, func(r *streams.RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{2, 3})))
			require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))
		}))
	})
	t.Run("after read returns an error", func(t *testing.T) {
		r := streams.NewRowReader(sec)
		require.NoError(t, r.Open(context.Background()))
		require.ErrorContains(t, r.MatchStreams(slices.Values([]int64{1})), "cannot change matched streams after reading has started")
	})
}

func idsOf(ss []streams.Stream) []int64 {
	ids := make([]int64, len(ss))
	for i, s := range ss {
		ids[i] = s.ID
	}
	return ids
}

// BenchmarkRowReader_MatchStreams reads a single-page streams section (so there is no page pruning —
// this isolates the per-row decode saving) while selecting a narrow (1 stream), wide (50%), and full
// (100%) set of the section's stream IDs, plus a no-filter baseline. Recorded stream IDs are 1..N.
func BenchmarkRowReader_MatchStreams(b *testing.B) {
	const n = 2000
	ctx := context.Background()
	sec := buildBenchStreamsSection(b, n)

	seq := func(count int) []int64 {
		ids := make([]int64, count)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		return ids
	}

	run := func(b *testing.B, match []int64) {
		b.Helper()
		r := streams.NewRowReader(sec)
		defer r.Close()
		buf := make([]streams.Stream, 512)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r.Reset(sec)
			if match != nil {
				if err := r.MatchStreams(slices.Values(match)); err != nil {
					b.Fatal(err)
				}
			}
			if err := r.Open(ctx); err != nil {
				b.Fatal(err)
			}
			for {
				nn, err := r.Read(ctx, buf)
				if err != nil && !errors.Is(err, io.EOF) {
					b.Fatal(err)
				}
				if nn == 0 && errors.Is(err, io.EOF) {
					break
				}
			}
		}
	}

	b.Run("narrow_1", func(b *testing.B) { run(b, seq(1)) })
	b.Run("wide_50pct", func(b *testing.B) { run(b, seq(n/2)) })
	b.Run("all_100pct", func(b *testing.B) { run(b, seq(n)) })
	b.Run("baseline_no_match", func(b *testing.B) { run(b, nil) })
}

// buildBenchStreamsSection builds a streams section with n distinct streams (IDs 1..n) in a single page
// (a large page size keeps every column in one page).
func buildBenchStreamsSection(b *testing.B, n int) *streams.Section {
	b.Helper()

	s := streams.NewBuilder(nil, 16<<20, 0) // large page size → single page
	for i := 0; i < n; i++ {
		s.Record(labels.FromStrings("app", "bench", "id", fmt.Sprintf("%06d", i)), unixTime(int64(i)), int64(i))
	}

	builder := dataobj.NewBuilder(nil)
	require.NoError(b, builder.Append(s))

	obj, closer, err := builder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { closer.Close() })

	sec, err := streams.Open(context.Background(), obj.Sections()[0])
	require.NoError(b, err)
	return sec
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

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
