package consumer

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// TestTOCAlignedMultiBuilder_AppendPreservesSharedStructuredMetadata asserts that splitting a
// stream across time windows keeps the shared structured metadata pool the stream carries once for
// all of its entries. Losing it here would silently drop every OTLP resource and scope attribute of
// any stream whose entries straddle a metastore window boundary.
func TestTOCAlignedMultiBuilder_AppendPreservesSharedStructuredMetadata(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	entryA := windowEntry(w1, time.Minute, "a")
	entryA.SharedResourceRef = 1
	entryB := windowEntry(w2, time.Minute, "b")
	entryB.SharedResourceRef = 1

	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{entryA, entryB},
		SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
			{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "myservice"}}},
		},
	}, w1))

	builders := m.GetBuilders()
	require.Len(t, builders, 2)

	// Both windows must carry the shared attribute on every one of their rows.
	for i, b := range builders {
		records := flushRecords(t, b)
		require.Lenf(t, records, 1, "window %d should hold exactly one entry", i)
		require.Equalf(t, "myservice", records[0].Metadata.Get("service_name"),
			"window %d lost the shared structured metadata", i)
	}
}

// TestTOCAlignedMultiBuilder_AppendPreservesSharedStructuredMetadataRefs asserts that the pool is
// propagated whole to every windowed stream, so that the 1-based references the entries carry still
// resolve to the same sets after the split. Compacting the pool per window - keeping only the sets
// the window's entries reference - would shift the indexes and hand entries somebody else's
// attributes.
func TestTOCAlignedMultiBuilder_AppendPreservesSharedStructuredMetadataRefs(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// The first window's entry references the first resource set and no scope set; the second
	// window's entry references the second resource set and the scope set. Only a whole pool keeps
	// both resolvable.
	first := windowEntry(w1, time.Minute, "first")
	first.SharedResourceRef = 1
	second := windowEntry(w2, time.Minute, "second")
	second.SharedResourceRef = 2
	second.SharedScopeRef = 3

	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{first, second},
		SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
			{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "one"}}},
			{Attrs: push.LabelsAdapter{{Name: "service_name", Value: "two"}}},
			{Attrs: push.LabelsAdapter{{Name: "scope_name", Value: "myscope"}}},
		},
	}, w1))

	// GetBuilders is ordered by window start time, so index 0 is w1 and index 1 is w2.
	builders := m.GetBuilders()
	require.Len(t, builders, 2)

	w1Records := flushRecords(t, builders[0])
	require.Len(t, w1Records, 1)
	require.Equal(t, "first", string(w1Records[0].Line))
	require.Equal(t, "one", w1Records[0].Metadata.Get("service_name"))
	require.Empty(t, w1Records[0].Metadata.Get("scope_name"))

	w2Records := flushRecords(t, builders[1])
	require.Len(t, w2Records, 1)
	require.Equal(t, "second", string(w2Records[0].Line))
	require.Equal(t, "two", w2Records[0].Metadata.Get("service_name"))
	require.Equal(t, "myscope", w2Records[0].Metadata.Get("scope_name"))
}

// flushRecords flushes a window builder and returns every log record it wrote. Record.Line and
// Record.Metadata are only valid until the next iteration, so they are copied out.
func flushRecords(t *testing.T, b builder) []logs.Record {
	t.Helper()

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var got []logs.Record
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		logsSection, err := logs.Open(t.Context(), sec)
		require.NoError(t, err)

		for res := range logs.IterSection(t.Context(), logsSection) {
			record, err := res.Value()
			require.NoError(t, err)
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
