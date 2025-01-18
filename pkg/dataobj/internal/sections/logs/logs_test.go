package logs_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
)

func Test(t *testing.T) {
	t.Skip("Disabled until sorting is reimplemented")

	records := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0).UTC(),
			Metadata:  nil,
			Line:      "hello world",
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0).UTC(),
			Metadata: push.LabelsAdapter{
				{Name: "cluster", Value: "test"},
				{Name: "app", Value: "bar"},
			},
			Line: "goodbye world",
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(5, 0).UTC(),
			Metadata: push.LabelsAdapter{
				{Name: "cluster", Value: "test"},
				{Name: "app", Value: "foo"},
			},
			Line: "foo bar",
		},
	}

	tracker := logs.New(nil, 1024)
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
			Metadata: push.LabelsAdapter{
				{Name: "app", Value: "foo"},
				{Name: "cluster", Value: "test"},
			},
			Line: "foo bar",
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0).UTC(),
			Metadata:  push.LabelsAdapter{},
			Line:      "hello world",
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0).UTC(),
			Metadata: push.LabelsAdapter{
				{Name: "app", Value: "bar"},
				{Name: "cluster", Value: "test"},
			},
			Line: "goodbye world",
		},
	}

	dec := encoding.ReadSeekerDecoder(bytes.NewReader(buf))

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
