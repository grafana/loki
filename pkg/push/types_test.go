package push

import (
	"encoding/json"
	"testing"
	time "time"

	"github.com/stretchr/testify/require"
)

var (
	now    = time.Now().UTC()
	line   = `level=info ts=2019-12-12T15:00:08.325Z caller=compact.go:441 component=tsdb msg="compact blocks" count=3 mint=1576130400000 maxt=1576152000000 ulid=01DVX9ZHNM71GRCJS7M34Q0EV7 sources="[01DVWNC6NWY1A60AZV3Z6DGS65 01DVWW7XXX75GHA6ZDTD170CSZ 01DVX33N5W86CWJJVRPAVXJRWJ]" duration=2.897213221s`
	stream = Stream{
		Labels: `{job="foobar", cluster="foo-central1", namespace="bar", container_name="buzz"}`,
		Hash:   1234*10 ^ 9,
		Entries: []Entry{
			{now, line, nil, nil, 0, 0},
			{now.Add(1 * time.Second), line, LabelsAdapter{{Name: "traceID", Value: "1234"}}, nil, 1, 2},
			{now.Add(2 * time.Second), line, nil, nil, 1, 0},
			{now.Add(3 * time.Second), line, LabelsAdapter{{Name: "user", Value: "abc"}}, LabelsAdapter{{Name: "msg", Value: "text"}}, 0, 2},
		},
		SharedStructuredMetadataSets: []SharedStructuredMetadataSet{
			{Attrs: []LabelAdapter{{Name: "service.name", Value: "svc"}}},
			{Attrs: []LabelAdapter{{Name: "scope.name", Value: "lib"}}},
		},
	}
	streamAdapter = StreamAdapter{
		Labels: `{job="foobar", cluster="foo-central1", namespace="bar", container_name="buzz"}`,
		Hash:   1234*10 ^ 9,
		Entries: []EntryAdapter{
			{now, line, nil, nil, 0, 0},
			{now.Add(1 * time.Second), line, []LabelPairAdapter{{Name: "traceID", Value: "1234"}}, nil, 1, 2},
			{now.Add(2 * time.Second), line, nil, nil, 1, 0},
			{now.Add(3 * time.Second), line, []LabelPairAdapter{{Name: "user", Value: "abc"}}, []LabelPairAdapter{{Name: "msg", Value: "text"}}, 0, 2},
		},
		SharedStructuredMetadataSets: []SharedStructuredMetadataSet{
			{Attrs: []LabelAdapter{{Name: "service.name", Value: "svc"}}},
			{Attrs: []LabelAdapter{{Name: "scope.name", Value: "lib"}}},
		},
	}
)

func TestStream(t *testing.T) {
	avg := testing.AllocsPerRun(200, func() {
		b, err := stream.Marshal()
		require.NoError(t, err)

		var new Stream
		err = new.Unmarshal(b)
		require.NoError(t, err)

		require.Equal(t, stream, new)
	})
	t.Log("avg allocs per run:", avg)
}

func TestStreamAdapter(t *testing.T) {
	avg := testing.AllocsPerRun(200, func() {
		b, err := streamAdapter.Marshal()
		require.NoError(t, err)

		var new StreamAdapter
		err = new.Unmarshal(b)
		require.NoError(t, err)

		require.Equal(t, streamAdapter, new)
	})
	t.Log("avg allocs per run:", avg)
}

func TestCompatibility(t *testing.T) {
	b, err := stream.Marshal()
	require.NoError(t, err)

	var adapter StreamAdapter
	err = adapter.Unmarshal(b)
	require.NoError(t, err)
	require.Equal(t, streamAdapter, adapter)

	ba, err := adapter.Marshal()
	require.NoError(t, err)
	require.Equal(t, b, ba)

	var new Stream
	err = new.Unmarshal(ba)
	require.NoError(t, err)

	require.Equal(t, stream, new)
}

// TestSharedStructuredMetadataSetsAbsent asserts that a stream carrying neither a pool nor
// any reference, which is what every non-OTLP producer writes, still round-trips cleanly on
// both codec paths.
func TestSharedStructuredMetadataSetsAbsent(t *testing.T) {
	noShared := stream
	noShared.SharedStructuredMetadataSets = nil
	noShared.Entries = make([]Entry, len(stream.Entries))
	copy(noShared.Entries, stream.Entries)
	for i := range noShared.Entries {
		noShared.Entries[i].SharedResourceRef = 0
		noShared.Entries[i].SharedScopeRef = 0
	}

	b, err := noShared.Marshal()
	require.NoError(t, err)

	var gotStream Stream
	require.NoError(t, gotStream.Unmarshal(b))
	require.Empty(t, gotStream.SharedStructuredMetadataSets)
	require.Equal(t, noShared, gotStream)

	var gotAdapter StreamAdapter
	require.NoError(t, gotAdapter.Unmarshal(b))
	require.Empty(t, gotAdapter.SharedStructuredMetadataSets)
	for _, e := range gotAdapter.Entries {
		require.Zero(t, e.SharedResourceRef)
		require.Zero(t, e.SharedScopeRef)
	}
}

// TestReservedFieldFourIsSkipped covers the retired flat sharedStructuredMetadata field.
// Field 4 of StreamAdapter used to hold a single LabelPairAdapter list shared by every entry
// of the stream. It never made it into a release, but scratch builds that emit it may still
// be around, so both codecs must decode such a payload without erroring and must discard the
// field rather than mistake it for something else.
func TestReservedFieldFourIsSkipped(t *testing.T) {
	// A LabelPairAdapter{Name: "service.name", Value: "svc"} as it sat in field 4, hand
	// assembled since no encoder in the tree emits it any more.
	legacyPair := []byte{
		0x0a, 0x0c, // field 1 (name), length 12
		's', 'e', 'r', 'v', 'i', 'c', 'e', '.', 'n', 'a', 'm', 'e',
		0x12, 0x03, // field 2 (value), length 3
		's', 'v', 'c',
	}

	payload := []byte{
		0x0a, 0x0b, // field 1 (labels), length 11
		'{', 'f', 'o', 'o', '=', '"', 'b', 'a', 'r', '"', '}',
		0x18, 0x07, // field 3 (hash), varint 7
	}
	payload = append(payload, 0x22, byte(len(legacyPair))) // field 4, length delimited
	payload = append(payload, legacyPair...)
	// One empty set in field 5, to prove decoding resumes correctly after the skip.
	payload = append(payload, 0x2a, 0x00)

	var gotStream Stream
	require.NoError(t, gotStream.Unmarshal(payload))
	require.Equal(t, `{foo="bar"}`, gotStream.Labels)
	require.Equal(t, uint64(7), gotStream.Hash)
	require.Len(t, gotStream.SharedStructuredMetadataSets, 1)
	require.Empty(t, gotStream.SharedStructuredMetadataSets[0].Attrs, "the retired field must not leak into the pool")

	var gotAdapter StreamAdapter
	require.NoError(t, gotAdapter.Unmarshal(payload))
	require.Equal(t, `{foo="bar"}`, gotAdapter.Labels)
	require.Equal(t, uint64(7), gotAdapter.Hash)
	require.Len(t, gotAdapter.SharedStructuredMetadataSets, 1)
	require.Empty(t, gotAdapter.SharedStructuredMetadataSets[0].Attrs)

	// Re-encoding must not resurrect the discarded bytes.
	reencoded, err := gotStream.Marshal()
	require.NoError(t, err)
	require.NotContains(t, string(reencoded), "service.name")
}

func TestStreamEqual(t *testing.T) {
	base := stream

	t.Run("equal to itself", func(t *testing.T) {
		other := stream
		require.True(t, base.Equal(&other))
	})

	t.Run("different pool contents", func(t *testing.T) {
		other := stream
		other.SharedStructuredMetadataSets = []SharedStructuredMetadataSet{
			{Attrs: []LabelAdapter{{Name: "service.name", Value: "other"}}},
			{Attrs: []LabelAdapter{{Name: "scope.name", Value: "lib"}}},
		}
		require.False(t, base.Equal(&other))
	})

	t.Run("different pool length", func(t *testing.T) {
		other := stream
		other.SharedStructuredMetadataSets = stream.SharedStructuredMetadataSets[:1]
		require.False(t, base.Equal(&other))
	})
}

func TestEntryEqual(t *testing.T) {
	base := Entry{
		Timestamp:         now,
		Line:              line,
		SharedResourceRef: 1,
		SharedScopeRef:    2,
	}

	same := base
	require.True(t, base.Equal(&same))

	otherResource := base
	otherResource.SharedResourceRef = 3
	require.False(t, base.Equal(&otherResource))

	otherScope := base
	otherScope.SharedScopeRef = 0
	require.False(t, base.Equal(&otherScope))

	// Swapping the two references is a real difference: they address sets that play
	// different roles.
	swapped := base
	swapped.SharedResourceRef, swapped.SharedScopeRef = base.SharedScopeRef, base.SharedResourceRef
	require.False(t, base.Equal(&swapped))
}

// TestSharedMetadataIsNotSerialisedToJSON pins the JSON shape of the shared metadata fields.
//
// All three are tagged `json:"-"`, mirroring Stream.Hash: they are internal to the OTLP ingest
// pipeline and only mean something next to the pool of their own stream, so letting one reach a
// query response would expose an internal index to clients.
//
// Stream.MarshalJSON already hides them for a *Stream, but it is defined on the pointer receiver,
// so marshalling a non-addressable Stream (a map value, an interface holding a value) bypasses it
// and falls back to the struct tags. That path is covered below.
func TestSharedMetadataIsNotSerialisedToJSON(t *testing.T) {
	b, err := json.Marshal(&stream)
	require.NoError(t, err)
	require.NotContains(t, string(b), "sharedStructuredMetadataSets")
	require.NotContains(t, string(b), "sharedResourceRef")
	require.NotContains(t, string(b), "sharedScopeRef")

	// Same for a value that MarshalJSON cannot be called on.
	byValue, err := json.Marshal(map[string]Stream{"s": stream})
	require.NoError(t, err)
	require.NotContains(t, string(byValue), "sharedStructuredMetadataSets")
	require.NotContains(t, string(byValue), "sharedResourceRef")
	require.NotContains(t, string(byValue), "sharedScopeRef")
	require.NotContains(t, string(byValue), "service.name", "the pool contents must not leak either")

	entry := Entry{
		Timestamp:         now,
		Line:              "hello",
		Parsed:            LabelsAdapter{{Name: "msg", Value: "text"}},
		SharedResourceRef: 1,
		SharedScopeRef:    2,
	}
	eb, err := json.Marshal(&entry)
	require.NoError(t, err)
	require.Contains(t, string(eb), "parsed", "sanity check that the other tags are honoured")
	require.NotContains(t, string(eb), "sharedResourceRef")
	require.NotContains(t, string(eb), "sharedScopeRef")
}

func BenchmarkStream(b *testing.B) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		by, err := stream.Marshal()
		if err != nil {
			b.Fatal(err)
		}
		var new Stream
		err = new.Unmarshal(by)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamAdapter(b *testing.B) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		by, err := streamAdapter.Marshal()
		if err != nil {
			b.Fatal(err)
		}
		var new StreamAdapter
		err = new.Unmarshal(by)
		if err != nil {
			b.Fatal(err)
		}
	}
}
