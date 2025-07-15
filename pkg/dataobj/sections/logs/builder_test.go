package logs_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

func Test(t *testing.T) {
	records := []logs.Record{
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  []labels.Label{{Name: "cluster", Value: "test"}, {Name: "app", Value: "foo"}},
			Line:      []byte("foo bar"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  nil,
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  []labels.Label{{Name: "cluster", Value: "test"}, {Name: "app", Value: "bar"}},
			Line:      []byte("goodbye world"),
		},
	}

	opts := logs.BuilderOptions{
		PageSizeHint:     1024,
		BufferSize:       256,
		StripeMergeLimit: 2,
	}

	tracker := logs.NewBuilder(nil, opts)
	for _, record := range records {
		tracker.Append(record)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

	// The order of records should be sorted by timestamp DESC then stream ID, and all
	// metadata should be sorted by key then value.
	expect := []logs.Record{
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  []labels.Label{{Name: "app", Value: "bar"}, {Name: "cluster", Value: "test"}},
			Line:      []byte("goodbye world"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  []labels.Label{},
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  []labels.Label{{Name: "app", Value: "foo"}, {Name: "cluster", Value: "test"}},
			Line:      []byte("foo bar"),
		},
	}

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf), int64(len(buf)))
	require.NoError(t, err)

	i := 0
	for result := range logs.Iter(context.Background(), obj) {
		record, err := result.Value()
		require.NoError(t, err)
		require.Equal(t, expect[i], record)
		i++
	}
}

func buildObject(lt *logs.Builder) ([]byte, error) {
	var buf bytes.Buffer

	builder := dataobj.NewBuilder()
	if err := builder.Append(lt); err != nil {
		return nil, err
	} else if _, err := builder.Flush(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
