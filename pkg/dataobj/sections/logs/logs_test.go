package logs_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

func Test(t *testing.T) {
	records := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  nil,
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  []logs.RecordMetadata{{Name: "cluster", Value: []byte("test")}, {Name: "app", Value: []byte("bar")}},
			Line:      []byte("goodbye world"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(5, 0),
			Metadata:  []logs.RecordMetadata{{Name: "cluster", Value: []byte("test")}, {Name: "app", Value: []byte("foo")}},
			Line:      []byte("foo bar"),
		},
	}

	opts := logs.Options{
		PageSizeHint:     1024,
		BufferSize:       256,
		StripeMergeLimit: 2,
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
			Timestamp: time.Unix(5, 0),
			Metadata:  []logs.RecordMetadata{{Name: "app", Value: []byte("foo")}, {Name: "cluster", Value: []byte("test")}},
			Line:      []byte("foo bar"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  []logs.RecordMetadata{},
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  []logs.RecordMetadata{{Name: "app", Value: []byte("bar")}, {Name: "cluster", Value: []byte("test")}},
			Line:      []byte("goodbye world"),
		},
	}

	dec := encoding.ReaderAtDecoder(bytes.NewReader(buf), int64(len(buf)))

	i := 0
	for result := range logs.Iter(context.Background(), dec) {
		record, err := result.Value()
		require.NoError(t, err)
		require.Equal(t, expect[i], record)
		i++
	}
}

func buildObject(lt *logs.Logs) ([]byte, error) {
	var buf bytes.Buffer
	enc := encoding.NewEncoder()
	if err := lt.EncodeTo(enc); err != nil {
		return nil, err
	} else if err := enc.Flush(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
