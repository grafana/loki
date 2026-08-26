package logproto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
)

func entry(ns int64, line string, md ...push.LabelAdapter) push.Entry {
	return push.Entry{
		Timestamp:          time.Unix(0, ns),
		Line:               line,
		StructuredMetadata: md,
	}
}

// TestEncodingsAreMutuallyUndecodable pins the property the field numbering of
// InternalStreamAdapter exists to provide: a record written in either encoding fails to
// decode as the other, so a consumer can attempt one and fall back on error rather than
// decoding to a stream with no entries and silently dropping every line.
//
// Giving Stream a varint field 2, or InternalStreamAdapter a length delimited one, would
// quietly remove the property, which is why it is asserted here rather than assumed.
func TestEncodingsAreMutuallyUndecodable(t *testing.T) {
	oneEntry := []push.Entry{entry(1, "x")}
	oneGroup := []ResourceLogs{{ScopeLogs: []ScopeLogs{{Entries: oneEntry}}}}

	marshalFlat := func(t *testing.T, s Stream) []byte {
		t.Helper()
		data, err := s.Marshal()
		require.NoError(t, err)
		return data
	}
	marshalNested := func(t *testing.T, s InternalStreamAdapter) []byte {
		t.Helper()
		data, err := s.Marshal()
		require.NoError(t, err)
		return data
	}

	tests := []struct {
		name          string
		data          func(*testing.T) []byte
		decodesFlat   bool
		decodesNested bool
	}{
		{
			name: "flat with entries and a hash",
			data: func(t *testing.T) []byte {
				return marshalFlat(t, Stream{Labels: `{a="b"}`, Hash: 7, Entries: oneEntry})
			},
			decodesFlat: true,
		},
		{
			name:        "flat with entries and a zero hash",
			data:        func(t *testing.T) []byte { return marshalFlat(t, Stream{Labels: `{a="b"}`, Entries: oneEntry}) },
			decodesFlat: true,
		},
		{
			name:        "flat with a hash and no entries",
			data:        func(t *testing.T) []byte { return marshalFlat(t, Stream{Labels: `{a="b"}`, Hash: 7}) },
			decodesFlat: true,
		},
		{
			name:        "flat with a single zero valued entry",
			data:        func(t *testing.T) []byte { return marshalFlat(t, Stream{Labels: `{a="b"}`, Entries: []push.Entry{{}}}) },
			decodesFlat: true,
		},
		{
			name: "nested with groups and a hash",
			data: func(t *testing.T) []byte {
				return marshalNested(t, InternalStreamAdapter{Labels: `{a="b"}`, Hash: 7, ResourceLogs: oneGroup})
			},
			decodesNested: true,
		},
		{
			name: "nested with groups and a zero hash",
			data: func(t *testing.T) []byte {
				return marshalNested(t, InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: oneGroup})
			},
			decodesNested: true,
		},
		{
			name: "nested with a single empty group",
			data: func(t *testing.T) []byte {
				return marshalNested(t, InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{}}})
			},
			decodesNested: true,
		},
		{
			// The only shape both accept, because neither carries a field the other
			// disagrees about. Asserted to decode the same either way below.
			name:          "labels alone",
			data:          func(t *testing.T) []byte { return marshalFlat(t, Stream{Labels: `{a="b"}`}) },
			decodesFlat:   true,
			decodesNested: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.data(t)

			var flat Stream
			flatErr := flat.Unmarshal(data)
			var nested InternalStreamAdapter
			nestedErr := nested.Unmarshal(data)

			require.Equal(t, tt.decodesFlat, flatErr == nil, "flat decode: %v", flatErr)
			require.Equal(t, tt.decodesNested, nestedErr == nil, "nested decode: %v", nestedErr)
			require.True(t, tt.decodesFlat || tt.decodesNested, "a record must decode as one of the two")

			if tt.decodesFlat && tt.decodesNested {
				require.Equal(t, flat, nested.ToStream(),
					"a record both encodings accept must mean the same thing either way")
			}
		})
	}
}

func TestFromStreamRoundTripsThroughToStream(t *testing.T) {
	tests := []struct {
		name   string
		stream Stream
	}{
		{"no entries", Stream{Labels: `{a="b"}`}},
		{"no entries with a hash", Stream{Labels: `{a="b"}`, Hash: 7}},
		{"one entry", Stream{Labels: `{a="b"}`, Hash: 7, Entries: []push.Entry{entry(1, "x")}}},
		{
			name: "entries with structured metadata",
			stream: Stream{Labels: `{a="b"}`, Hash: 7, Entries: []push.Entry{
				entry(1, "x", push.LabelAdapter{Name: "trace_id", Value: "1"}),
				entry(2, "y", push.LabelAdapter{Name: "trace_id", Value: "2"}),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nested := FromStream(tt.stream)
			require.Equal(t, len(tt.stream.Entries), nested.EntryCount())
			require.Equal(t, tt.stream, nested.ToStream())
		})
	}
}

// TestToStreamLeavesEntriesNilWhenEmpty keeps a decoded record indistinguishable from the
// flat form, which unmarshals to a nil slice rather than an empty one when it carries no
// entries.
func TestToStreamLeavesEntriesNilWhenEmpty(t *testing.T) {
	for _, s := range []InternalStreamAdapter{
		{Labels: `{a="b"}`},
		{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{}}},
		{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{ScopeLogs: []ScopeLogs{{}}}}},
	} {
		require.Nil(t, s.ToStream().Entries)
	}
}

// TestToStreamResolvesEffectiveMetadata pins the expansion against what the OTLP parse site
// produces when it flattens attributes onto entries itself: the entry's own pairs, then the
// resource's, then the scope's, neither sorted nor deduplicated. Anything else and a nested
// record would store different bytes than its flat equivalent.
func TestToStreamResolvesEffectiveMetadata(t *testing.T) {
	nested := InternalStreamAdapter{
		Labels: `{a="b"}`,
		Hash:   7,
		ResourceLogs: []ResourceLogs{{
			Attrs: []push.LabelAdapter{{Name: "host", Value: "host-1"}, {Name: "shared", Value: "resource"}},
			ScopeLogs: []ScopeLogs{{
				Attrs: []push.LabelAdapter{{Name: "scope", Value: "lib"}, {Name: "shared", Value: "scope"}},
				Entries: []push.Entry{
					entry(1, "x", push.LabelAdapter{Name: "shared", Value: "entry"}),
					entry(2, "y"),
				},
			}},
		}},
	}

	got := nested.ToStream()

	require.Equal(t, `{a="b"}`, got.Labels)
	require.Equal(t, uint64(7), got.Hash)
	require.Len(t, got.Entries, 2)

	require.Equal(t, push.LabelsAdapter{
		{Name: "shared", Value: "entry"},
		{Name: "host", Value: "host-1"},
		{Name: "shared", Value: "resource"},
		{Name: "scope", Value: "lib"},
		{Name: "shared", Value: "scope"},
	}, got.Entries[0].StructuredMetadata)

	require.Equal(t, push.LabelsAdapter{
		{Name: "host", Value: "host-1"},
		{Name: "shared", Value: "resource"},
		{Name: "scope", Value: "lib"},
		{Name: "shared", Value: "scope"},
	}, got.Entries[1].StructuredMetadata)
}

// TestToStreamKeepsEntriesUnderTheirOwnGroup guards the association that containment
// carries: an entry must not inherit the attributes of a resource it does not sit under.
func TestToStreamKeepsEntriesUnderTheirOwnGroup(t *testing.T) {
	nested := InternalStreamAdapter{
		Labels: `{a="b"}`,
		ResourceLogs: []ResourceLogs{
			{
				Attrs:     []push.LabelAdapter{{Name: "host", Value: "host-1"}},
				ScopeLogs: []ScopeLogs{{Entries: []push.Entry{entry(1, "one")}}},
			},
			{
				Attrs:     []push.LabelAdapter{{Name: "host", Value: "host-2"}},
				ScopeLogs: []ScopeLogs{{Entries: []push.Entry{entry(2, "two")}}},
			},
		},
	}

	got := nested.ToStream()

	require.Len(t, got.Entries, 2)
	require.Equal(t, "one", got.Entries[0].Line)
	require.Equal(t, push.LabelsAdapter{{Name: "host", Value: "host-1"}}, got.Entries[0].StructuredMetadata)
	require.Equal(t, "two", got.Entries[1].Line)
	require.Equal(t, push.LabelsAdapter{{Name: "host", Value: "host-2"}}, got.Entries[1].StructuredMetadata)
}

// TestToStreamDoesNotWriteThroughSharedAttrs guards against expanding into a shared slice. The
// signature of AppendEffectiveMetadata invites passing resAttrs as dst to save an allocation,
// but a resource's attributes belong to every entry beneath it, so appending one entry's
// metadata onto them would corrupt its siblings.
func TestToStreamDoesNotWriteThroughSharedAttrs(t *testing.T) {
	resAttrs := make([]push.LabelAdapter, 1, 8) // spare capacity is what makes a stray append silent
	resAttrs[0] = push.LabelAdapter{Name: "host", Value: "host-1"}

	nested := InternalStreamAdapter{
		Labels: `{a="b"}`,
		ResourceLogs: []ResourceLogs{{
			Attrs: resAttrs,
			ScopeLogs: []ScopeLogs{{
				Attrs:   []push.LabelAdapter{{Name: "scope", Value: "lib"}},
				Entries: []push.Entry{entry(1, "x"), entry(2, "y")},
			}},
		}},
	}

	nested.ToStream()

	require.Equal(t, []push.LabelAdapter{{Name: "host", Value: "host-1"}}, nested.ResourceLogs[0].Attrs)
	require.Equal(t, []push.LabelAdapter{{Name: "scope", Value: "lib"}}, nested.ResourceLogs[0].ScopeLogs[0].Attrs)
}
