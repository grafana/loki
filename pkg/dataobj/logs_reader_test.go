package dataobj_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
)

var recordsTestdata = []logs.Record{
	{StreamID: 1, Timestamp: unixTime(10), Metadata: nil, Line: []byte("hello")},
	{StreamID: 1, Timestamp: unixTime(15), Metadata: labels.FromStrings("trace_id", "123"), Line: []byte("world")},
	{StreamID: 2, Timestamp: unixTime(5), Metadata: nil, Line: []byte("hello again")},
	{StreamID: 2, Timestamp: unixTime(20), Metadata: labels.FromStrings("user", "12"), Line: []byte("world again")},
	{StreamID: 3, Timestamp: unixTime(25), Metadata: labels.FromStrings("user", "14"), Line: []byte("hello one more time")},
	{StreamID: 3, Timestamp: unixTime(30), Metadata: labels.FromStrings("trace_id", "123"), Line: []byte("world one more time")},
}

func metadata(kvps ...string) push.LabelsAdapter {
	if len(kvps)%2 != 0 {
		panic("metadata: odd number of key-value pairs")
	}

	m := make(push.LabelsAdapter, len(kvps)/2)
	for i := 0; i < len(kvps); i += 2 {
		m = append(m, push.LabelAdapter{Name: kvps[i], Value: kvps[i+1]})
	}
	return m
}

func TestLogsReader(t *testing.T) {
	expect := []dataobj.Record{
		{1, unixTime(10), labels.FromStrings(), []byte("hello")},
		{1, unixTime(15), labels.FromStrings("trace_id", "123"), []byte("world")},
		{2, unixTime(5), labels.FromStrings(), []byte("hello again")},
		{2, unixTime(20), labels.FromStrings("user", "12"), []byte("world again")},
		{3, unixTime(25), labels.FromStrings("user", "14"), []byte("hello one more time")},
		{3, unixTime(30), labels.FromStrings("trace_id", "123"), []byte("world one more time")},
	}

	// Build with many pages but one section.
	obj := buildLogsObject(t, logs.Options{
		PageSizeHint: 1,
		BufferSize:   1,
		SectionSize:  1024,
	})
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.LogsSections)

	r := dataobj.NewLogsReader(obj, 0)
	actual, err := readAllRecords(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestLogsReader_MatchStreams(t *testing.T) {
	expect := []dataobj.Record{
		{1, unixTime(10), labels.FromStrings(), []byte("hello")},
		{1, unixTime(15), labels.FromStrings("trace_id", "123"), []byte("world")},
		{3, unixTime(25), labels.FromStrings("user", "14"), []byte("hello one more time")},
		{3, unixTime(30), labels.FromStrings("trace_id", "123"), []byte("world one more time")},
	}

	// Build with many pages but one section.
	obj := buildLogsObject(t, logs.Options{
		PageSizeHint: 1,
		BufferSize:   1,
		SectionSize:  1024,
	})
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.LogsSections)

	r := dataobj.NewLogsReader(obj, 0)
	require.NoError(t, r.MatchStreams(slices.Values([]int64{1, 3})))

	actual, err := readAllRecords(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestLogsReader_AddMetadataMatcher(t *testing.T) {
	expect := []dataobj.Record{
		{1, unixTime(15), labels.FromStrings("trace_id", "123"), []byte("world")},
		{3, unixTime(30), labels.FromStrings("trace_id", "123"), []byte("world one more time")},
	}

	// Build with many pages but one section.
	obj := buildLogsObject(t, logs.Options{
		PageSizeHint: 1,
		BufferSize:   1,
		SectionSize:  1024,
	})
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.LogsSections)

	r := dataobj.NewLogsReader(obj, 0)
	require.NoError(t, r.SetPredicate(dataobj.MetadataMatcherPredicate{"trace_id", "123"}))

	actual, err := readAllRecords(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func TestLogsReader_AddMetadataFilter(t *testing.T) {
	expect := []dataobj.Record{
		{2, unixTime(20), labels.FromStrings("user", "12"), []byte("world again")},
		{3, unixTime(25), labels.FromStrings("user", "14"), []byte("hello one more time")},
	}

	// Build with many pages but one section.
	obj := buildLogsObject(t, logs.Options{
		PageSizeHint: 1,
		BufferSize:   1,
		SectionSize:  1024,
	})
	md, err := obj.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.LogsSections)

	r := dataobj.NewLogsReader(obj, 0)
	err = r.SetPredicate(dataobj.MetadataFilterPredicate{
		Key: "user",
		Keep: func(key, value string) bool {
			require.Equal(t, "user", key)
			return strings.HasPrefix(value, "1")
		},
	})
	require.NoError(t, err)

	actual, err := readAllRecords(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

func buildLogsObject(t *testing.T, opts logs.Options) *dataobj.Object {
	t.Helper()

	s := logs.New(nil, opts)
	for _, rec := range recordsTestdata {
		s.Append(rec)
	}

	var buf bytes.Buffer

	enc := encoding.NewEncoder(&buf)
	require.NoError(t, s.EncodeTo(enc))
	require.NoError(t, enc.Flush())

	return dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

func readAllRecords(ctx context.Context, r *dataobj.LogsReader) ([]dataobj.Record, error) {
	var (
		res []dataobj.Record
		buf = make([]dataobj.Record, 128)
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

// BenchmarkLogsReader benchmarks the performance of reading 10k log lines with structured metadata from a data object.
func BenchmarkLogsReader(b *testing.B) {
	// Build the object
	opts := dataobj.BuilderConfig{
		TargetObjectSize:  16 * 1024 * 1024,
		TargetSectionSize: 4 * 1024 * 1024,
		BufferSize:        16 * 1024 * 1024,
		TargetPageSize:    2 * 1024 * 1024,
	}

	builder, err := dataobj.NewBuilder(opts)
	require.NoError(b, err)

	for i := 0; i < 10000; i++ {
		err := builder.Append(push.Stream{
			Labels: "{cluster=\"test\",app=\"bar\"}",
			Entries: []push.Entry{
				{
					Timestamp: time.Now().Add(time.Duration(i) * time.Second),
					Line:      "hello world " + strconv.Itoa(i),
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "123"},
						{Name: "pod", Value: "pod-abcd"},
					},
				},
			},
		})
		require.NoError(b, err)
	}

	var buf bytes.Buffer
	_, err = builder.Flush(&buf)
	require.NoError(b, err)

	obj := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(len(buf.Bytes())))

	md, err := obj.Metadata(context.Background())
	require.NoError(b, err)
	require.Equal(b, 1, md.LogsSections)

	r := dataobj.NewLogsReader(obj, 0)

	var (
		recs = make([]dataobj.Record, 128)
		ctx  = context.Background()
	)
	b.ResetTimer()
	b.ReportAllocs()
	// Read all the logs we just wrote
	cnt := 0
	for i := 0; i < b.N; i++ {
		for {
			n, err := r.Read(ctx, recs)
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			require.NoError(b, err)
			cnt += n
		}
		r.Reset(obj, 0)
		require.Equal(b, 10000, cnt)
		cnt = 0
	}
}
