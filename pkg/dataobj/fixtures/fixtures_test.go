package fixtures

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestLogRecords(t *testing.T) {
	lr := NewLogsFixtureBuilder(t)
	lr.ForStream(`{app="foo"}`).
		Entry(10, `{trace_id="123"}`, "foo").
		Entry(15, `{trace_id="456"}`, "foo again")
	lr.ForStream(`{app="bar"}`).
		Entry(20, `{trace_id="abc"}`, "bar").
		Entry(25, `{trace_id="def"}`, "bar again")

	obj, closer := DataObject(t,
		LogsSection(t, "tenant", lr.Logs()),
		StreamsSection(t, "tenant", lr.Streams()),
	)
	t.Cleanup(func() { closer.Close() })

	requireEqualStreams(t, obj)
	requireEqualLogs(t, obj)
}

func requireEqualStreams(t *testing.T, obj *dataobj.Object) {
	// Verify streams
	expectedStreams := []streams.Stream{
		{
			ID:               1,
			MinTimestamp:     time.Unix(10, 0).UTC(),
			MaxTimestamp:     time.Unix(15, 0).UTC(),
			UncompressedSize: 18,
			Labels:           labels.FromStrings("app", "foo"),
			Rows:             2,
			ShardBucket:      16,
		},
		{
			ID:               2,
			MinTimestamp:     time.Unix(20, 0).UTC(),
			MaxTimestamp:     time.Unix(25, 0).UTC(),
			UncompressedSize: 18,
			Labels:           labels.FromStrings("app", "bar"),
			Rows:             2,
			ShardBucket:      19,
		},
	}

	var actualStreams []streams.Stream
	for res := range streams.Iter(t.Context(), obj) {
		rec := res.MustValue()
		actualStreams = append(actualStreams, rec)
	}
	require.Equal(t, expectedStreams, actualStreams)
}

func requireEqualLogs(t *testing.T, obj *dataobj.Object) {
	// Verify logs: stream ASC, timestamp DESC
	expectedRecords := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(15, 0).UTC(),
			Metadata:  labels.FromStrings("trace_id", "456"),
			Line:      []byte("foo again"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0).UTC(),
			Metadata:  labels.FromStrings("trace_id", "123"),
			Line:      []byte("foo"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(25, 0).UTC(),
			Metadata:  labels.FromStrings("trace_id", "def"),
			Line:      []byte("bar again"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(20, 0).UTC(),
			Metadata:  labels.FromStrings("trace_id", "abc"),
			Line:      []byte("bar"),
		},
	}

	var actualRecords []logs.Record
	for res := range logs.Iter(t.Context(), obj) {
		rec := res.MustValue()
		actualRecords = append(actualRecords, rec.Copy())
	}

	require.Equal(t, expectedRecords, actualRecords)
}
