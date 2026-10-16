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

func TestReadTenantStreamsAndLogs(t *testing.T) {
	first := NewLogsFixtureBuilder(t)
	first.ForStream(`{app="first"}`).Entry(10, "{}", "one")
	second := NewLogsFixtureBuilder(t)
	second.ForStream(`{app="second"}`).Entry(11, "{}", "two")
	other := NewLogsFixtureBuilder(t)
	other.ForStream(`{app="other"}`).Entry(12, "{}", "skip")

	obj, closer := DataObject(t,
		StreamsSection(t, "tenant", first.Streams()),
		LogsSection(t, "tenant", first.Logs()),
		StreamsSection(t, "other", other.Streams()),
		LogsSection(t, "other", other.Logs()),
		StreamsSection(t, "tenant", second.Streams()),
		LogsSection(t, "tenant", second.Logs()),
	)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	gotStreams := ReadTenantStreams(t, obj, "tenant")
	require.Len(t, gotStreams, 2)
	require.Equal(t, "first", gotStreams[0].Labels.Get("app"))
	require.Equal(t, "second", gotStreams[1].Labels.Get("app"))

	gotLogs := ReadTenantLogs(t, obj, "tenant")
	require.Equal(t, []string{"one", "two"}, []string{string(gotLogs[0].Line), string(gotLogs[1].Line)})
	require.Equal(t, "two", string(ReadTenantLogSection(t, obj, "tenant", 2)[0].Line))
	gotLogs[0].Line[0] = 'X'
	require.Equal(t, "one", string(ReadTenantLogSection(t, obj, "tenant", 0)[0].Line))
}

func TestLogFixtureBuilder_SchemaLabels(t *testing.T) {
	b := NewLogsFixtureBuilder(t, WithSchemaLabels("label:cluster", "label:app"))
	b.ForStream(`{app="api",cluster="prod"}`).Entry(10, "{}", "first")
	b.ForStream(`{app="worker",cluster="dev"}`).Entry(11, "{}", "second")
	b.ForStream(`{app="api",cluster="prod"}`).Entry(12, "{}", "third")

	require.Equal(t, []string{"prod\x00api", "dev\x00worker", "prod\x00api"}, []string{
		b.Logs()[0].SchemaKey,
		b.Logs()[1].SchemaKey,
		b.Logs()[2].SchemaKey,
	})

	withoutSchema := NewLogsFixtureBuilder(t)
	withoutSchema.ForStream(`{app="api"}`).Entry(10, "{}", "no key")
	require.Empty(t, withoutSchema.Logs()[0].SchemaKey)
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
