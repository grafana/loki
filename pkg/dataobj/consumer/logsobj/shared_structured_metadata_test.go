package logsobj

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestBuilder_AppendSharedStructuredMetadata asserts that structured metadata a stream carries
// once for all of its entries (the OTLP resource and scope attributes, when their expansion is
// deferred) ends up on every row of the written object.
//
// The columnar builders are not attribute aware yet, so the builder materializes the effective
// view of every entry. Once they are, this test should assert that the attributes are encoded once
// per stream instead - but they must still be readable per row.
func TestBuilder_AppendSharedStructuredMetadata(t *testing.T) {
	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels: `{cluster="test",app="foo"}`,
		Entries: []push.Entry{
			{
				Timestamp: time.Unix(10, 0).UTC(),
				Line:      "hello",
				StructuredMetadata: push.LabelsAdapter{
					{Name: "trace_id", Value: "123"},
				},
				SharedResourceRef: 1,
				SharedScopeRef:    2,
			},
			{
				// No own structured metadata at all: this entry must still get the shared sets.
				Timestamp:         time.Unix(20, 0).UTC(),
				Line:              "hello again",
				SharedResourceRef: 1,
				SharedScopeRef:    2,
			},
		},
		SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
			{Attrs: push.LabelsAdapter{
				{Name: "service_name", Value: "myservice"},
				{Name: "deployment_environment", Value: "prod"},
			}},
			{Attrs: push.LabelsAdapter{
				{Name: "scope_name", Value: "myscope"},
			}},
		},
	}

	require.NoError(t, builder.Append("tenant", stream, time.Now()))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	records := readAllLogRecords(t, obj)
	require.Len(t, records, len(stream.Entries))

	byLine := make(map[string]logs.Record, len(records))
	for _, record := range records {
		byLine[string(record.Line)] = record
	}

	// The entry that had its own metadata keeps it and gains the referenced sets.
	first, ok := byLine["hello"]
	require.True(t, ok)
	require.Equal(t, "123", first.Metadata.Get("trace_id"))
	require.Equal(t, "myservice", first.Metadata.Get("service_name"))
	require.Equal(t, "prod", first.Metadata.Get("deployment_environment"))
	require.Equal(t, "myscope", first.Metadata.Get("scope_name"))

	// The entry that had none gets exactly the referenced sets.
	second, ok := byLine["hello again"]
	require.True(t, ok)
	require.Equal(t, "myservice", second.Metadata.Get("service_name"))
	require.Equal(t, "prod", second.Metadata.Get("deployment_environment"))
	require.Equal(t, "myscope", second.Metadata.Get("scope_name"))

	// Appending must not have mutated the entries: the shared sets are owned by the stream and
	// are read-only for the builder.
	require.Equal(t, push.LabelsAdapter{{Name: "trace_id", Value: "123"}}, stream.Entries[0].StructuredMetadata)
	require.Nil(t, stream.Entries[1].StructuredMetadata)
}

// TestBuilder_AppendSharedStructuredMetadataPerEntryRefs asserts that entries of the same stream
// referencing different sets of its pool each get the sets they reference and nothing else, and
// that a zero reference means no set.
func TestBuilder_AppendSharedStructuredMetadataPerEntryRefs(t *testing.T) {
	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels: `{cluster="test",app="foo"}`,
		Entries: []push.Entry{
			{
				Timestamp:         time.Unix(10, 0).UTC(),
				Line:              "resource one, scope one",
				SharedResourceRef: 1,
				SharedScopeRef:    3,
			},
			{
				Timestamp:         time.Unix(20, 0).UTC(),
				Line:              "resource two, no scope",
				SharedResourceRef: 2,
			},
			{
				// Both references are the "none" reference.
				Timestamp: time.Unix(30, 0).UTC(),
				Line:      "no shared sets",
			},
		},
		SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
			{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "one"}}},
			{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "two"}}},
			{Attrs: push.LabelsAdapter{{Name: "scope_name", Value: "myscope"}}},
		},
	}

	require.NoError(t, builder.Append("tenant", stream, time.Now()))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	byLine := make(map[string]logs.Record, len(stream.Entries))
	for _, record := range readAllLogRecords(t, obj) {
		byLine[string(record.Line)] = record
	}
	require.Len(t, byLine, len(stream.Entries))

	first := byLine["resource one, scope one"]
	require.Equal(t, "one", first.Metadata.Get("service_name"))
	require.Equal(t, "myscope", first.Metadata.Get("scope_name"))

	second := byLine["resource two, no scope"]
	require.Equal(t, "two", second.Metadata.Get("service_name"))
	require.Empty(t, second.Metadata.Get("scope_name"))

	third := byLine["no shared sets"]
	require.Empty(t, third.Metadata.Get("service_name"))
	require.Empty(t, third.Metadata.Get("scope_name"))
}

// TestBuilder_SharedStructuredMetadataCountsTowardsSize asserts that the shared structured
// metadata is included in the per entry size the streams section records, consistently with how
// the entries' own structured metadata is counted there.
func TestBuilder_SharedStructuredMetadataCountsTowardsSize(t *testing.T) {
	entryWithoutShared := push.Entry{
		Timestamp: time.Unix(10, 0).UTC(),
		Line:      "hello",
	}
	entryWithShared := entryWithoutShared
	entryWithShared.SharedResourceRef = 1

	shared := []logproto.SharedStructuredMetadataSet{
		{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "myservice"}}},
	}

	sizeOf := func(t *testing.T, stream logproto.Stream) int64 {
		t.Helper()

		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)
		require.NoError(t, builder.Append("tenant", stream, time.Now()))

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		var total int64
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				stream, err := res.Value()
				require.NoError(t, err)
				total += stream.UncompressedSize
			}
		}
		return total
	}

	withShared := sizeOf(t, logproto.Stream{
		Labels:                       `{cluster="test",app="foo"}`,
		Entries:                      []push.Entry{entryWithShared},
		SharedStructuredMetadataSets: shared,
	})
	withoutShared := sizeOf(t, logproto.Stream{
		Labels:  `{cluster="test",app="foo"}`,
		Entries: []push.Entry{entryWithoutShared},
	})

	require.Equal(t, withoutShared+int64(len("myservice")), withShared,
		"the shared structured metadata values must be counted the same way the per entry ones are")
}

func readAllLogRecords(t *testing.T, obj *dataobj.Object) []logs.Record {
	t.Helper()

	var got []logs.Record
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		for res := range iterLogsSection(t, sec) {
			record, err := res.Value()
			require.NoError(t, err)
			// Record.Line and Record.Metadata are only valid until the next iteration, so copy
			// what the assertions need out of them.
			got = append(got, logs.Record{
				StreamID:  record.StreamID,
				Timestamp: record.Timestamp,
				Metadata:  record.Metadata.Copy(),
				Line:      append([]byte(nil), record.Line...),
			})
		}
	}
	return got
}
