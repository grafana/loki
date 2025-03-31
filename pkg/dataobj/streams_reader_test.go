package dataobj_test

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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
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

func TestStreamsReader(t *testing.T) {
	expect := []dataobj.Stream{
		{1, unixTime(10), unixTime(15), 25, labels.FromStrings("cluster", "test", "app", "foo")},
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz")},
	}

	obj := buildStreamsObject(t, 1) // Many pages
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.StreamsSections)

	r := dataobj.NewStreamsReader(obj, 0)
	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestStreamsReader_AddLabelMatcher(t *testing.T) {
	expect := []dataobj.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar")},
	}

	obj := buildStreamsObject(t, 1) // Many pages
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.StreamsSections)

	r := dataobj.NewStreamsReader(obj, 0)
	require.NoError(t, r.SetPredicate(dataobj.LabelMatcherPredicate{Name: "app", Value: "bar"}))

	actual, err := readAllStreams(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestStreamsReader_AddLabelFilter(t *testing.T) {
	expect := []dataobj.Stream{
		{2, unixTime(5), unixTime(20), 45, labels.FromStrings("cluster", "test", "app", "bar")},
		{3, unixTime(25), unixTime(30), 35, labels.FromStrings("cluster", "test", "app", "baz")},
	}

	obj := buildStreamsObject(t, 1) // Many pages
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.StreamsSections)

	r := dataobj.NewStreamsReader(obj, 0)
	err = r.SetPredicate(dataobj.LabelFilterPredicate{
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

func unixTime(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

func buildStreamsObject(t *testing.T, pageSize int) *dataobj.Object {
	t.Helper()

	s := streams.New(nil, pageSize)
	for _, d := range streamsTestdata {
		s.Record(d.Labels, d.Timestamp, d.UncompressedSize)
	}

	var buf bytes.Buffer

	enc := encoding.NewEncoder(&buf)
	require.NoError(t, s.EncodeTo(enc))
	require.NoError(t, enc.Flush())

	return dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

func readAllStreams(ctx context.Context, r *dataobj.StreamsReader) ([]dataobj.Stream, error) {
	var (
		res []dataobj.Stream
		buf = make([]dataobj.Stream, 128)
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
