package streams_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestRowReader(t *testing.T) {
	expect := []streams.Stream{
		{1, unixTime(10), unixTime(15), 25, labels.FromStrings("cluster", "test", "app", "foo"), 2},
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2},
	}

	sec := buildStreamsSection(t, 1) // Many pages
	r := streams.NewRowReader(sec)
	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_AddLabelMatcher(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2},
	}

	sec := buildStreamsSection(t, 1) // Many pages
	r := streams.NewRowReader(sec)
	require.NoError(t, r.SetPredicate(streams.LabelMatcherRowPredicate{Name: "app", Value: "bar"}))

	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestRowReader_AddLabelFilter(t *testing.T) {
	expect := []streams.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar"), 2},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz"), 2},
	}

	sec := buildStreamsSection(t, 1) // Many pages
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

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func buildStreamsSection(t *testing.T, pageSize int) *streams.Section {
	t.Helper()

	s := streams.NewBuilder(nil, pageSize)
	for _, d := range streamsTestdata {
		s.Record(d.Labels, d.Timestamp, d.UncompressedSize)
	}

	var buf bytes.Buffer

	builder := dataobj.NewBuilder()
	require.NoError(t, builder.Append(s))

	_, err := builder.Flush(&buf)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	sec, err := streams.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllStreams(ctx context.Context, r *streams.RowReader) ([]streams.Stream, error) {
	var (
		res []streams.Stream
		buf = make([]streams.Stream, 128)
	)

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
