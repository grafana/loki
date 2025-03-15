package logs_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
)

func Test(t *testing.T) {
	records := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0).UTC(),
			Metadata:  nil,
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0).UTC(),
			Metadata:  labels.FromStrings("cluster", "test", "app", "bar"),
			Line:      []byte("goodbye world"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(5, 0).UTC(),
			Metadata:  labels.FromStrings("cluster", "test", "app", "foo"),
			Line:      []byte("foo bar"),
		},
	}

	opts := logs.Options{
		PageSizeHint: 1024,
		BufferSize:   256,
		SectionSize:  4096,
	}

	tracker := logs.New(nil, opts)
	for _, record := range records {
		tracker.Append(record)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

	// The order of records should be sorted by stream ID then timestamp, and all
	// metadata should be sorted by key then value.
	expect := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(5, 0).UTC(),
			Metadata: labels.FromStrings(
				"app", "foo",
				"cluster", "test",
			),
			Line: []byte("foo bar"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0).UTC(),
			Metadata:  labels.FromStrings(),
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0).UTC(),
			Metadata:  labels.FromStrings("app", "bar", "cluster", "test"),
			Line:      []byte("goodbye world"),
		},
	}

	dec := encoding.ReaderAtDecoder(bytes.NewReader(buf), int64(len(buf)))

	var actual []logs.Record
	for result := range logs.Iter(context.Background(), dec) {
		record, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, record)
	}

	require.Equal(t, expect, actual)
}

func buildObject(lt *logs.Logs) ([]byte, error) {
	var buf bytes.Buffer
	enc := encoding.NewEncoder(&buf)
	if err := lt.EncodeTo(enc); err != nil {
		return nil, err
	} else if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
